// Package wsx is postctl's WebSocket client. It turns a WebSocket request from
// the model into a handshake (variables, inherited auth, cookies from the
// workspace jar), holds the connection, and reports everything that happens as
// a stream of timestamped events: the handshake, every message in both
// directions with its size, pings and pongs, errors and the close.
//
// The package knows nothing about the terminal. [Prepare] resolves a request,
// [Prepared.Dial] connects, and [Conn.Next] delivers events:
//
//	prep, err := wsx.Prepare(ctx, wsx.PrepareInput{Collection: c, Request: r, Environment: env})
//	conn, err := prep.Dial(ctx)
//	go func() {
//		for {
//			ev, err := conn.Next(ctx)
//			if err != nil { return } // io.EOF once the close event was delivered
//			...
//		}
//	}()
//	_ = conn.Send(ctx, wsx.Text, []byte("hello"))
//	_ = conn.Close(1000, "bye")
//
// Cancelling the context passed to Dial closes the connection. A handshake the
// server refuses is a [*HandshakeError] carrying the status and a bounded part
// of the body. Errors never contain the URL's query string or user info.
package wsx

import (
	"net/http"
	"time"
)

// Direction says which way an event travelled.
type Direction int

// Event directions.
const (
	// DirSystem is for events that are not traffic: open, close, errors.
	DirSystem Direction = iota
	// DirIn is traffic received from the server.
	DirIn
	// DirOut is traffic sent by the client.
	DirOut
)

// String returns "system", "in" or "out".
func (d Direction) String() string {
	switch d {
	case DirIn:
		return "in"
	case DirOut:
		return "out"
	}
	return "system"
}

// MessageType is the frame type of a data message.
type MessageType int

// Message types.
const (
	// Text is a UTF-8 text frame.
	Text MessageType = iota + 1
	// Binary is a binary frame.
	Binary
)

// String returns "text" or "binary".
func (m MessageType) String() string {
	if m == Binary {
		return "binary"
	}
	return "text"
}

// EventType classifies an [Event].
type EventType int

// Event types.
const (
	// EventOpen is the successful handshake; Event.Info describes it.
	EventOpen EventType = iota + 1
	// EventMessage is a data message in either direction.
	EventMessage
	// EventPing is a ping frame in either direction.
	EventPing
	// EventPong is a pong frame in either direction.
	EventPong
	// EventError is a problem that did not close the connection, such as a
	// failed send. Event.Ref is the failed message's sequence number, if any.
	EventError
	// EventClose is always the last event: Code and Reason are the close
	// status, Err is set when the connection was lost abnormally.
	EventClose
)

// String returns a short lower-case name.
func (t EventType) String() string {
	switch t {
	case EventOpen:
		return "open"
	case EventMessage:
		return "message"
	case EventPing:
		return "ping"
	case EventPong:
		return "pong"
	case EventError:
		return "error"
	case EventClose:
		return "close"
	}
	return "unknown"
}

// Close status codes used by this package (RFC 6455 §7.4.1).
const (
	// CloseNormal is a normal closure.
	CloseNormal = 1000
	// CloseGoingAway means the endpoint is going away.
	CloseGoingAway = 1001
	// CloseNoStatus means a close frame carried no status.
	CloseNoStatus = 1005
	// CloseAbnormal means the connection dropped without a close frame.
	CloseAbnormal = 1006
)

// Info describes an open connection.
type Info struct {
	// URL is the handshake URL without user info or query string.
	URL string
	// Status is the handshake response status (101).
	Status int
	// Subprotocol is the protocol the server selected, or "".
	Subprotocol string
	// Header is the handshake response header.
	Header http.Header
}

// Event is one thing that happened on a connection. Events are delivered in
// order by [Conn.Next].
type Event struct {
	// Seq numbers events from 1 in the order they were recorded.
	Seq uint64
	// Time is when the event was recorded.
	Time time.Time
	// Type classifies the event.
	Type EventType
	// Dir is the direction of traffic events; DirSystem otherwise.
	Dir Direction
	// Msg is the frame type of an [EventMessage].
	Msg MessageType
	// Data is the message payload, or the ping/pong payload. For a sent
	// message it is the display form given to [WithDisplay] when there is one
	// (secret values masked), otherwise the bytes sent.
	Data []byte
	// Size is the payload size on the wire in bytes.
	Size int
	// Latency is the round-trip time of the ping a received [EventPong]
	// answers; zero when unknown.
	Latency time.Duration
	// Code and Reason are the close status of an [EventClose].
	Code   int
	Reason string
	// Err is the cause of an [EventError], or of an abnormal [EventClose].
	Err error
	// Ref is the sequence number of the message an [EventError] is about.
	Ref uint64
	// Info describes the connection for an [EventOpen].
	Info *Info
}
