package llmproxy

import (
	"database/sql"
	"encoding/json"
	"strings"

	_ "modernc.org/sqlite"
)

// Store 是请求日志的 SQLite 存储。
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  upstream TEXT, method TEXT, path TEXT,
  status INTEGER, duration_ms INTEGER, ttft_ms INTEGER, stream INTEGER,
  req_bytes INTEGER, resp_bytes INTEGER,
  model TEXT, prompt_tokens INTEGER, completion_tokens INTEGER, total_tokens INTEGER,
  tag TEXT, error TEXT,
  req_headers TEXT, resp_headers TEXT,
  req_body TEXT, resp_body TEXT, resp_merged TEXT,
  req_truncated INTEGER, resp_truncated INTEGER
);
CREATE INDEX IF NOT EXISTS idx_requests_ts ON requests(ts DESC);
`

func openStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// SQLite 写串行;单连接最省心,LLM 调用量级远谈不上瓶颈。
	db.SetMaxOpenConns(1)
	// WAL 让读不阻塞写;busy_timeout 让多实例/并发下的写锁自动重试,而不是立刻"database is locked"。
	for _, pragma := range []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	// 老库迁移:补上后加的列(CREATE TABLE IF NOT EXISTS 不会给已存在的表加列)。
	ensureColumn(db, "ttft_ms", "INTEGER DEFAULT 0")
	// 一次性回填:早期写入的行(mergeSSE/usage 尚不支持 Responses API 时)token 记成了 0,
	// 用最新逻辑从已存响应体重算,让概览/花费对历史数据也有意义。user_version 保证只跑一次。
	backfillUsage(db)
	return &Store{db: db}, nil
}

// backfillUsage 用最新的 extractUsage/mergeSSE/extractModel 回填 token=0 的历史行(只跑一次)。
func backfillUsage(db *sql.DB) {
	var ver int
	if err := db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil || ver >= 2 {
		return
	}
	rows, err := db.Query(`SELECT id,stream,req_body,resp_body FROM requests
	  WHERE prompt_tokens=0 AND completion_tokens=0 AND total_tokens=0`)
	if err != nil {
		return
	}
	type rec struct {
		id     int64
		stream int
		req    string
		resp   string
	}
	var recs []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.stream, &r.req, &r.resp); err != nil {
			rows.Close()
			return
		}
		recs = append(recs, r)
	}
	rows.Close()

	for _, r := range recs {
		stream := r.stream != 0
		p, c, t := extractUsage(r.resp, stream)
		if p == 0 && c == 0 && t == 0 {
			continue
		}
		if model := extractModel(r.req, r.resp); model != "" {
			_, _ = db.Exec("UPDATE requests SET prompt_tokens=?,completion_tokens=?,total_tokens=?,model=CASE WHEN model='' THEN ? ELSE model END WHERE id=?",
				p, c, t, model, r.id)
		} else {
			_, _ = db.Exec("UPDATE requests SET prompt_tokens=?,completion_tokens=?,total_tokens=? WHERE id=?", p, c, t, r.id)
		}
		if stream {
			if m := mergeSSE(r.resp); m != "" {
				_, _ = db.Exec("UPDATE requests SET resp_merged=? WHERE id=?", m, r.id)
			}
		}
	}
	_, _ = db.Exec("PRAGMA user_version = 2")
}

// ensureColumn 若 requests 表缺某列则补上(已存在会报错,忽略即可)。
func ensureColumn(db *sql.DB, name, decl string) {
	rows, err := db.Query("PRAGMA table_info(requests)")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var cname, ctype string
		var dflt any
		if err := rows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
			return
		}
		if cname == name {
			return // 已存在
		}
	}
	_, _ = db.Exec("ALTER TABLE requests ADD COLUMN " + name + " " + decl)
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Insert 写入一条记录,返回自增 id。
func (s *Store) Insert(c *capture) (int64, error) {
	reqH, _ := json.Marshal(c.reqHeaders)
	respH, _ := json.Marshal(c.respHeaders)
	res, err := s.db.Exec(`INSERT INTO requests
	  (ts,upstream,method,path,status,duration_ms,ttft_ms,stream,req_bytes,resp_bytes,model,
	   prompt_tokens,completion_tokens,total_tokens,tag,error,
	   req_headers,resp_headers,req_body,resp_body,resp_merged,req_truncated,resp_truncated)
	  VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ts, c.upstream, c.method, c.path, c.status, c.durationMs, c.ttftMs, b2i(c.stream),
		c.reqBytes, c.respBytes, c.model, c.promptTok, c.completeTok, c.totalTok,
		c.tag, c.errMsg, string(reqH), string(respH), c.reqBody, c.respBody, c.respMerged,
		b2i(c.reqTrunc), b2i(c.respTrunc))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Update 用响应结果回填一条已插入的记录(配合"请求到达即插入"的两段式写入)。
func (s *Store) Update(id int64, c *capture) error {
	respH, _ := json.Marshal(c.respHeaders)
	_, err := s.db.Exec(`UPDATE requests SET
	  status=?, duration_ms=?, ttft_ms=?, stream=?, resp_bytes=?, model=?,
	  prompt_tokens=?, completion_tokens=?, total_tokens=?, error=?,
	  resp_headers=?, resp_body=?, resp_merged=?, resp_truncated=?
	  WHERE id=?`,
		c.status, c.durationMs, c.ttftMs, b2i(c.stream), c.respBytes, c.model,
		c.promptTok, c.completeTok, c.totalTok, c.errMsg,
		string(respH), c.respBody, c.respMerged, b2i(c.respTrunc), id)
	return err
}

// Query 列表(按时间倒序),返回当页 + 总数。
func (s *Store) Query(q LogQuery) (*LogPage, error) {
	where, args := buildWhere(q)
	page := &LogPage{Items: []LogEntry{}}

	if err := s.db.QueryRow("SELECT COUNT(*) FROM requests "+where, args...).Scan(&page.Total); err != nil {
		return nil, err
	}

	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id,ts,upstream,method,path,status,duration_ms,ttft_ms,stream,
	  req_bytes,resp_bytes,model,prompt_tokens,completion_tokens,total_tokens,tag,error
	  FROM requests `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, limit, q.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e LogEntry
		var stream int
		if err := rows.Scan(&e.ID, &e.TS, &e.Upstream, &e.Method, &e.Path, &e.Status,
			&e.DurationMs, &e.TTFTMs, &stream, &e.ReqBytes, &e.RespBytes, &e.Model,
			&e.PromptTokens, &e.CompletionTokens, &e.TotalTokens, &e.Tag, &e.Error); err != nil {
			return nil, err
		}
		e.Stream = stream != 0
		page.Items = append(page.Items, e)
	}
	return page, rows.Err()
}

// Detail 单条详情(含头与体)。
//
// 列语义:resp_body 恒为原始响应(非流=完整响应、流=原始 SSE);resp_merged 为流合并文本。
// 对外语义:RespBody=可读(非流→原始、流→合并),RespRaw=原始 SSE(仅流式有值)。
func (s *Store) Detail(id int64) (*LogDetail, error) {
	var d LogDetail
	var stream, reqTrunc, respTrunc int
	var reqH, respH, rawResp, merged string
	row := s.db.QueryRow(`SELECT id,ts,upstream,method,path,status,duration_ms,ttft_ms,stream,
	  req_bytes,resp_bytes,model,prompt_tokens,completion_tokens,total_tokens,tag,error,
	  req_headers,resp_headers,req_body,resp_body,resp_merged,req_truncated,resp_truncated
	  FROM requests WHERE id=?`, id)
	e := &d.Entry
	if err := row.Scan(&e.ID, &e.TS, &e.Upstream, &e.Method, &e.Path, &e.Status,
		&e.DurationMs, &e.TTFTMs, &stream, &e.ReqBytes, &e.RespBytes, &e.Model,
		&e.PromptTokens, &e.CompletionTokens, &e.TotalTokens, &e.Tag, &e.Error,
		&reqH, &respH, &d.ReqBody, &rawResp, &merged, &reqTrunc, &respTrunc); err != nil {
		return nil, err
	}
	e.Stream = stream != 0
	d.ReqTruncated = reqTrunc != 0
	d.RespTruncated = respTrunc != 0
	_ = json.Unmarshal([]byte(reqH), &d.ReqHeaders)
	_ = json.Unmarshal([]byte(respH), &d.RespHeaders)
	if e.Stream {
		d.RespRaw = rawResp
		// 读取时用最新逻辑重新合并(改进 mergeSSE 后历史记录也能受益);合不出再退回落库时的值。
		if m := mergeSSE(rawResp); m != "" {
			d.RespBody = m
		} else {
			d.RespBody = merged
		}
	} else {
		d.RespBody = rawResp
	}
	return &d, nil
}

func buildWhere(q LogQuery) (string, []any) {
	var conds []string
	var args []any
	if q.Upstream != "" {
		conds = append(conds, "upstream=?")
		args = append(args, q.Upstream)
	}
	if q.Method != "" {
		conds = append(conds, "method=?")
		args = append(args, strings.ToUpper(q.Method))
	}
	switch q.Status {
	case "2xx":
		conds = append(conds, "status>=200 AND status<300")
	case "4xx":
		conds = append(conds, "status>=400 AND status<500")
	case "5xx":
		conds = append(conds, "status>=500")
	case "error":
		conds = append(conds, "error!=''")
	case "":
	default:
		if n := atoiSafe(q.Status); n > 0 {
			conds = append(conds, "status=?")
			args = append(args, n)
		}
	}
	if q.Search != "" {
		conds = append(conds, "(path LIKE ? OR model LIKE ?)")
		like := "%" + q.Search + "%"
		args = append(args, like, like)
	}
	if len(conds) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec("DELETE FROM requests WHERE id=?", id)
	return err
}

func (s *Store) Clear() error {
	_, err := s.db.Exec("DELETE FROM requests")
	return err
}

// Purge 删除早于 beforeMs 的记录,返回删除条数。
func (s *Store) Purge(beforeMs int64) (int64, error) {
	res, err := s.db.Exec("DELETE FROM requests WHERE ts < ?", beforeMs)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
