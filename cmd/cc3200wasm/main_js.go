// SPDX-License-Identifier: GPL-2.0-only
//go:build js && wasm

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall/js"
	"time"

	"goteddycloud/internal/cc3200"
)

type browserLink struct{ transport js.Value }
type promiseResult struct {
	value js.Value
	err   error
}

func await(ctx context.Context, promise js.Value) (js.Value, error) {
	result := make(chan promiseResult, 1)
	var success, failure js.Func
	success = js.FuncOf(func(_ js.Value, args []js.Value) any {
		value := js.Undefined()
		if len(args) > 0 {
			value = args[0]
		}
		result <- promiseResult{value: value}
		return nil
	})
	failure = js.FuncOf(func(_ js.Value, args []js.Value) any {
		message := "browser serial operation failed"
		if len(args) > 0 {
			message = args[0].String()
			if args[0].Type() == js.TypeObject && args[0].Get("message").Type() == js.TypeString {
				message = args[0].Get("message").String()
			}
		}
		result <- promiseResult{err: errors.New(message)}
		return nil
	})
	promise.Call("then", success, failure)
	select {
	case r := <-result:
		success.Release()
		failure.Release()
		return r.value, r.err
	case <-ctx.Done():
		// The transport settles after its read timeout/disconnect. Release callbacks
		// then, rather than leaving JS with references to already released Go funcs.
		go func() { <-result; success.Release(); failure.Release() }()
		return js.Undefined(), ctx.Err()
	}
}
func (b browserLink) Read(ctx context.Context, n int, timeout time.Duration) ([]byte, error) {
	value, err := await(ctx, b.transport.Call("read", n, timeout.Milliseconds()))
	if err != nil {
		return nil, err
	}
	if value.Get("byteLength").Int() != n {
		return nil, errors.New("short browser serial response")
	}
	result := make([]byte, n)
	if js.CopyBytesToGo(result, value) != n {
		return nil, errors.New("invalid browser serial bytes")
	}
	return result, nil
}
func (b browserLink) Write(ctx context.Context, data []byte) error {
	_, err := await(ctx, b.transport.Call("write", uint8Array(data)))
	return err
}
func (b browserLink) Flush(ctx context.Context) error {
	_, err := await(ctx, b.transport.Call("flush"))
	return err
}
func (b browserLink) Break(ctx context.Context) error {
	_, err := await(ctx, b.transport.Call("break"))
	return err
}
func (b browserLink) Reset(ctx context.Context, boot bool) error {
	_, err := await(ctx, b.transport.Call("reset", boot))
	return err
}
func uint8Array(data []byte) js.Value {
	value := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(value, data)
	return value
}
func byteArg(value js.Value, limit int) ([]byte, error) {
	if value.Type() != js.TypeObject || value.Get("byteLength").Type() != js.TypeNumber {
		return nil, errors.New("expected Uint8Array")
	}
	size := value.Get("byteLength").Int()
	if size < 0 || size > limit {
		return nil, errors.New("browser input exceeds limit")
	}
	result := make([]byte, size)
	if js.CopyBytesToGo(result, value) != size {
		return nil, errors.New("invalid byte input")
	}
	return result, nil
}
func promise(work func() (any, error)) js.Value {
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolve, reject := args[0], args[1]
		go func() {
			defer func() {
				if r := recover(); r != nil {
					reject.Invoke(js.Global().Get("Error").New(fmt.Sprint(r)))
				}
			}()
			value, err := work()
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(err.Error()))
			} else {
				resolve.Invoke(value)
			}
		}()
		return nil
	})
	result := js.Global().Get("Promise").New(executor)
	executor.Release()
	return result
}
func newClient(_ js.Value, args []js.Value) any {
	transport := args[0]
	client := cc3200.NewClient(browserLink{transport: transport})
	ctx, cancel := context.WithCancel(context.Background())
	object := js.Global().Get("Object").New()
	var functions []js.Func
	closed := false
	register := func(name string, work func([]js.Value) (any, error)) {
		callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
			return promise(func() (any, error) {
				if closed {
					return nil, errors.New("CC3200 client is closed")
				}
				return work(args)
			})
		})
		functions = append(functions, callback)
		object.Set(name, callback)
	}
	register("prepare", func(args []js.Value) (any, error) {
		loader, err := byteArg(args[0], 128<<10)
		if err != nil {
			return nil, err
		}
		return nil, client.Prepare(ctx, loader)
	})
	register("readFile", func(args []js.Value) (any, error) {
		data, err := client.ReadFile(ctx, args[0].String())
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return uint8Array(data), nil
	})
	register("fileInfo", func(args []js.Value) (any, error) {
		info, err := client.FileInfo(ctx, args[0].String())
		if err != nil {
			return nil, err
		}
		if !info.Exists {
			return nil, nil
		}
		return info.Size, nil
	})
	register("writeFile", func(args []js.Value) (any, error) {
		data, err := byteArg(args[1], cc3200.MaxFlash)
		if err != nil {
			return nil, err
		}
		return nil, client.WriteFile(ctx, args[0].String(), data, cc3200.WriteOptions{Verify: true})
	})
	register("rawFlash", func(args []js.Value) (any, error) {
		var progress cc3200.Progress
		if len(args) > 0 && args[0].Type() == js.TypeFunction {
			progress = func(done, total int) { args[0].Invoke(done, total) }
		}
		data, err := client.ReadFlash(ctx, 0, 0, progress)
		if err != nil {
			return nil, err
		}
		return uint8Array(data), nil
	})
	register("close", func(_ []js.Value) (any, error) {
		closed = true
		cancel()
		for _, f := range functions {
			f.Release()
		}
		return nil, nil
	})
	return object
}
func main() { factory := js.FuncOf(newClient); js.Global().Set("cc3200Go", factory); select {} }
