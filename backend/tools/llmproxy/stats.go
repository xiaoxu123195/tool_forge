package llmproxy

import (
	"sort"
	"strings"
	"time"
)

// Stats 聚合指定时间范围内的请求(用于概览仪表盘)。行数在个人代理量级下不大,直接拉出来在 Go 里算,
// 便于同时得到分位数、按模型/上游/天 的多维聚合。
func (s *Store) Stats(q StatsQuery) (*Stats, error) {
	var conds []string
	var args []any
	if q.Since > 0 {
		conds = append(conds, "ts >= ?")
		args = append(args, q.Since)
	}
	if q.Until > 0 {
		conds = append(conds, "ts <= ?")
		args = append(args, q.Until)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	rows, err := s.db.Query(`SELECT ts,upstream,model,status,error,stream,
	  duration_ms,ttft_ms,prompt_tokens,completion_tokens,total_tokens
	  FROM requests `+where+` ORDER BY ts`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	st := &Stats{Models: []ModelStat{}, Upstreams: []UpstreamStat{}, Timeline: []DayBucket{}}
	type magg struct {
		m              *ModelStat
		durSum, durN   int
		ttftSum, ttftN int
	}
	models := map[string]*magg{}
	ups := map[string]*UpstreamStat{}
	days := map[string]*DayBucket{}
	var dur, ttft []int

	for rows.Next() {
		var ts int64
		var upstream, model, errMsg string
		var status, stream, durationMs, ttftMs, promptTok, completeTok, totalTok int
		if err := rows.Scan(&ts, &upstream, &model, &status, &errMsg, &stream,
			&durationMs, &ttftMs, &promptTok, &completeTok, &totalTok); err != nil {
			return nil, err
		}
		total := totalTok
		if total == 0 {
			total = promptTok + completeTok
		}
		isErr := status >= 400 || errMsg != ""

		st.Requests++
		if isErr {
			st.Errors++
		}
		if stream != 0 {
			st.Streamed++
		}
		st.PromptTokens += promptTok
		st.CompletionTokens += completeTok
		st.TotalTokens += total
		if durationMs > 0 {
			dur = append(dur, durationMs)
		}
		if ttftMs > 0 {
			ttft = append(ttft, ttftMs)
		}

		// 按模型
		mkey := model
		if mkey == "" {
			mkey = "(未知)"
		}
		ma := models[mkey]
		if ma == nil {
			ma = &magg{m: &ModelStat{Model: mkey}}
			models[mkey] = ma
		}
		ma.m.Requests++
		if isErr {
			ma.m.Errors++
		}
		ma.m.PromptTokens += promptTok
		ma.m.CompletionTokens += completeTok
		ma.m.TotalTokens += total
		if durationMs > 0 {
			ma.durSum += durationMs
			ma.durN++
		}
		if ttftMs > 0 {
			ma.ttftSum += ttftMs
			ma.ttftN++
		}

		// 按上游
		ua := ups[upstream]
		if ua == nil {
			ua = &UpstreamStat{Upstream: upstream}
			ups[upstream] = ua
		}
		ua.Requests++
		if isErr {
			ua.Errors++
		}
		ua.TotalTokens += total

		// 按天(本地时区)
		day := time.UnixMilli(ts).Format("2006-01-02")
		db := days[day]
		if db == nil {
			db = &DayBucket{Date: day}
			days[day] = db
		}
		db.Requests++
		db.TotalTokens += total
		if isErr {
			db.Errors++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	st.AvgDurationMs = avg(dur)
	st.P50DurationMs = percentile(dur, 0.50)
	st.P95DurationMs = percentile(dur, 0.95)
	st.AvgTTFTMs = avg(ttft)
	st.P95TTFTMs = percentile(ttft, 0.95)

	for _, ma := range models {
		ma.m.AvgDurationMs = div(ma.durSum, ma.durN)
		ma.m.AvgTTFTMs = div(ma.ttftSum, ma.ttftN)
		st.Models = append(st.Models, *ma.m)
	}
	sort.Slice(st.Models, func(i, j int) bool {
		if st.Models[i].TotalTokens != st.Models[j].TotalTokens {
			return st.Models[i].TotalTokens > st.Models[j].TotalTokens
		}
		return st.Models[i].Requests > st.Models[j].Requests
	})

	for _, u := range ups {
		st.Upstreams = append(st.Upstreams, *u)
	}
	sort.Slice(st.Upstreams, func(i, j int) bool { return st.Upstreams[i].Requests > st.Upstreams[j].Requests })

	for _, d := range days {
		st.Timeline = append(st.Timeline, *d)
	}
	sort.Slice(st.Timeline, func(i, j int) bool { return st.Timeline[i].Date < st.Timeline[j].Date })

	return st, nil
}

func avg(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	sum := 0
	for _, x := range xs {
		sum += x
	}
	return sum / len(xs)
}

func div(sum, n int) int {
	if n == 0 {
		return 0
	}
	return sum / n
}

// percentile 最近邻分位(p∈[0,1]);会就地排序传入切片。
func percentile(xs []int, p float64) int {
	if len(xs) == 0 {
		return 0
	}
	sort.Ints(xs)
	idx := int(float64(len(xs)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(xs) {
		idx = len(xs) - 1
	}
	return xs[idx]
}
