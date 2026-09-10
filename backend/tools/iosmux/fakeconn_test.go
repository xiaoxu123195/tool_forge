package iosmux

import (
	"io"
	"net"
	"time"
)

// fakeConn 只用来喂给 recv 一段固定字节,别的方法都不会被碰到
type fakeConn struct{ r io.Reader }

func (c fakeConn) Read(b []byte) (int, error)         { return c.r.Read(b) }
func (c fakeConn) Write(b []byte) (int, error)        { return len(b), nil }
func (c fakeConn) Close() error                       { return nil }
func (c fakeConn) LocalAddr() net.Addr                { return nil }
func (c fakeConn) RemoteAddr() net.Addr               { return nil }
func (c fakeConn) SetDeadline(t time.Time) error      { return nil }
func (c fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (c fakeConn) SetWriteDeadline(t time.Time) error { return nil }
