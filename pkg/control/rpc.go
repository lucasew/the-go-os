package control

import (
	"fmt"
	"io"
	"net"

	controlv1 "github.com/lucasew/the-go-os/pkg/control/v1"
	"google.golang.org/protobuf/proto"
)

// Engine is implemented by the supervisor.
type Engine interface {
	ListUnits() ([]*controlv1.UnitInfo, error)
	StartUnit(name string) error
	StopUnit(name string) error
	RestartUnit(name string) error
	StatusUnit(name string) (*controlv1.UnitInfo, error)
	DaemonReload() error
}

// ServeConn handles one client connection (request/response loop until EOF).
func ServeConn(conn net.Conn, eng Engine) error {
	defer conn.Close()
	for {
		raw, err := ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		var req controlv1.Request
		if err := proto.Unmarshal(raw, &req); err != nil {
			if werr := writeErr(conn, fmt.Errorf("bad request: %w", err)); werr != nil {
				return werr
			}
			continue
		}
		resp := handle(eng, &req)
		out, err := proto.Marshal(resp)
		if err != nil {
			return err
		}
		if err := WriteFrame(conn, out); err != nil {
			return err
		}
	}
}

func handle(eng Engine, req *controlv1.Request) *controlv1.Response {
	resp := &controlv1.Response{}
	switch op := req.Op.(type) {
	case *controlv1.Request_ListUnits:
		units, err := eng.ListUnits()
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Result = &controlv1.Response_ListUnits{ListUnits: &controlv1.ListUnitsResponse{Units: units}}
	case *controlv1.Request_StartUnit:
		if err := eng.StartUnit(op.StartUnit.GetName()); err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Result = &controlv1.Response_Ok{Ok: &controlv1.Empty{}}
	case *controlv1.Request_StopUnit:
		if err := eng.StopUnit(op.StopUnit.GetName()); err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Result = &controlv1.Response_Ok{Ok: &controlv1.Empty{}}
	case *controlv1.Request_RestartUnit:
		if err := eng.RestartUnit(op.RestartUnit.GetName()); err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Result = &controlv1.Response_Ok{Ok: &controlv1.Empty{}}
	case *controlv1.Request_StatusUnit:
		info, err := eng.StatusUnit(op.StatusUnit.GetName())
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Result = &controlv1.Response_StatusUnit{StatusUnit: &controlv1.StatusUnitResponse{Unit: info}}
	case *controlv1.Request_DaemonReload:
		if err := eng.DaemonReload(); err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Result = &controlv1.Response_Ok{Ok: &controlv1.Empty{}}
	default:
		resp.Error = "empty or unknown request"
	}
	return resp
}

func writeErr(w io.Writer, err error) error {
	resp := &controlv1.Response{Error: err.Error()}
	out, mErr := proto.Marshal(resp)
	if mErr != nil {
		return mErr
	}
	return WriteFrame(w, out)
}

// Call sends one request and reads one response.
func Call(conn net.Conn, req *controlv1.Request) (*controlv1.Response, error) {
	raw, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := WriteFrame(conn, raw); err != nil {
		return nil, err
	}
	in, err := ReadFrame(conn)
	if err != nil {
		return nil, err
	}
	var resp controlv1.Response
	if err := proto.Unmarshal(in, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Dial opens the control socket.
func Dial() (net.Conn, error) {
	return net.Dial("unix", SocketPath())
}
