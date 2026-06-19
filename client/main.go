//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall/js"
	"time"

	"github.com/samcharles93/NellDB"
	"github.com/samcharles93/NellDB/sdk"
)

var (
	store nell.Store
	db    *sdk.DocDB
	ctx   = context.Background()

	// authSecret is set by nellSetAuth and applied to sync calls.
	authSecret []byte

	// Active replicators keyed by an integer handle so JS can stop them.
	replicators   = make(map[int]*sdk.Replicator)
	repStopFuncs  = make(map[int]func())
	nextRepHandle int
)

// asyncPromise wraps a Go function in a JS Promise.
// It returns a js.Value representing the Promise.
func asyncPromise(fn func() (any, error)) js.Value {
	promiseConstructor := js.Global().Get("Promise")
	var executor js.Func
	executor = js.FuncOf(func(this js.Value, args []js.Value) any {
		resolve := args[0]
		reject := args[1]
		go func() {
			defer executor.Release()
			res, err := fn()
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(err.Error()))
			} else {
				resolve.Invoke(res)
			}
		}()
		return nil
	})
	return promiseConstructor.New(executor)
}

func registerCallbacks() {
	// ── Write ──────────────────────────────────────────────────────────────
	js.Global().Set("nellPut", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return asyncPromise(func() (any, error) { return nil, fmt.Errorf("missing argument") })
		}
		docStr := args[0].String()

		return asyncPromise(func() (any, error) {
			var doc sdk.Doc
			if err := json.Unmarshal([]byte(docStr), &doc); err != nil {
				return errorJSON(err.Error()), nil
			}
			rev, err := db.Put(ctx, doc)
			if err != nil {
				return errorJSON(err.Error()), nil
			}
			resp, _ := json.Marshal(map[string]any{
				"ok":  true,
				"rev": rev,
			})
			return string(resp), nil
		})
	}))

	// ── Read ──────────────────────────────────────────────────────────────
	js.Global().Set("nellGet", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return asyncPromise(func() (any, error) { return nil, fmt.Errorf("missing id") })
		}
		id := args[0].String()

		return asyncPromise(func() (any, error) {
			doc, err := db.Get(ctx, id)
			if err != nil {
				return errorJSON(err.Error()), nil
			}
			resp, _ := json.Marshal(map[string]any{"ok": true, "doc": doc})
			return string(resp), nil
		})
	}))

	// ── Remove ────────────────────────────────────────────────────────────
	js.Global().Set("nellRemove", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return asyncPromise(func() (any, error) { return nil, fmt.Errorf("missing argument") })
		}
		argStr := args[0].String()

		return asyncPromise(func() (any, error) {
			var idOrDoc any
			if argStr != "" && argStr[0] == '{' {
				var doc sdk.Doc
				if err := json.Unmarshal([]byte(argStr), &doc); err == nil {
					idOrDoc = doc
				} else {
					idOrDoc = argStr
				}
			} else {
				idOrDoc = argStr
			}

			rev, err := db.Remove(ctx, idOrDoc)
			if err != nil {
				return errorJSON(err.Error()), nil
			}
			resp, _ := json.Marshal(map[string]any{"ok": true, "rev": rev})
			return string(resp), nil
		})
	}))

	// ── AllDocs ───────────────────────────────────────────────────────────
	js.Global().Set("nellAllDocs", js.FuncOf(func(this js.Value, args []js.Value) any {
		var rng sdk.DocRange
		if len(args) > 0 && args[0].Type() == js.TypeString && args[0].String() != "" {
			if err := json.Unmarshal([]byte(args[0].String()), &rng); err != nil {
				return asyncPromise(func() (any, error) { return errorJSON(err.Error()), nil })
			}
		}

		return asyncPromise(func() (any, error) {
			res, err := db.AllDocs(ctx, rng)
			if err != nil {
				return errorJSON(err.Error()), nil
			}
			resp, _ := json.Marshal(map[string]any{"ok": true, "result": res})
			return string(resp), nil
		})
	}))

	// ── Sync ──────────────────────────────────────────────────────────────
	js.Global().Set("nellSync", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return asyncPromise(func() (any, error) { return nil, fmt.Errorf("missing serverUrl") })
		}
		serverUrl := args[0].String()

		return asyncPromise(func() (any, error) {
			rep := sdk.NewReplicator(db, serverUrl)
			if len(authSecret) > 0 {
				rep.SetAuthSecret(authSecret)
			}
			pushed, pulled, err := rep.Sync(ctx)
			if err != nil {
				return nil, err
			}
			resp, _ := json.Marshal(map[string]any{
				"ok":     true,
				"pushed": pushed,
				"pulled": pulled,
			})
			return string(resp), nil
		})
	}))

	// ── Search Similar ────────────────────────────────────────────────────
	js.Global().Set("nellSearchSimilar", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return asyncPromise(func() (any, error) { return nil, fmt.Errorf("missing arguments") })
		}

		vectorStr := args[0].String()
		limit := args[1].Int()

		return asyncPromise(func() (any, error) {
			var vector []float32
			if err := json.Unmarshal([]byte(vectorStr), &vector); err != nil {
				return errorJSON(err.Error()), nil
			}

			docs, err := db.SearchSimilar(ctx, vector, limit)
			if err != nil {
				return errorJSON(err.Error()), nil
			}

			resp, _ := json.Marshal(map[string]any{"ok": true, "docs": docs})
			return string(resp), nil
		})
	}))

	// ── PutMany (bulk insert) ─────────────────────────────────────────────
	js.Global().Set("nellPutMany", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return asyncPromise(func() (any, error) { return nil, fmt.Errorf("missing argument") })
		}
		docsStr := args[0].String()

		return asyncPromise(func() (any, error) {
			var docs []sdk.Doc
			if err := json.Unmarshal([]byte(docsStr), &docs); err != nil {
				return errorJSON(err.Error()), nil
			}
			revs, err := db.PutMany(ctx, docs)
			if err != nil {
				return errorJSON(err.Error()), nil
			}
			resp, _ := json.Marshal(map[string]any{"ok": true, "revs": revs})
			return string(resp), nil
		})
	}))

	// ── GetMany (bulk fetch) ──────────────────────────────────────────────
	js.Global().Set("nellGetMany", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return asyncPromise(func() (any, error) { return nil, fmt.Errorf("missing argument") })
		}
		idsStr := args[0].String()

		return asyncPromise(func() (any, error) {
			var ids []string
			if err := json.Unmarshal([]byte(idsStr), &ids); err != nil {
				return errorJSON(err.Error()), nil
			}
			docs, err := db.GetMany(ctx, ids)
			if err != nil {
				return errorJSON(err.Error()), nil
			}
			resp, _ := json.Marshal(map[string]any{"ok": true, "docs": docs})
			return string(resp), nil
		})
	}))

	// ── Changes feed ──────────────────────────────────────────────────────
	// Starts a goroutine that forwards changes to the JS callback.  Returns
	// a handle that can be passed to nellStopChanges to cancel.
	js.Global().Set("nellChanges", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(-1)
		}
		callback := args[0]

		changesCtx, cancel := context.WithCancel(ctx)
		ch := db.Changes(changesCtx)

		go func() {
			for change := range ch {
				data, _ := json.Marshal(change)
				callback.Invoke(string(data))
			}
			cancel()
		}()

		handle := nextRepHandle
		nextRepHandle++
		repStopFuncs[handle] = cancel
		return js.ValueOf(handle)
	}))

	js.Global().Set("nellStopChanges", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.Undefined()
		}
		handle := args[0].Int()
		if stop, ok := repStopFuncs[handle]; ok {
			stop()
			delete(repStopFuncs, handle)
		}
		return js.Undefined()
	}))

	// ── Live sync (HTTP polling) ──────────────────────────────────────────
	// Starts continuous push+pull on an interval.  Returns a handle that
	// can be passed to nellStopSync to stop the loop.
	js.Global().Set("nellLiveSync", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(-1)
		}
		serverUrl := args[0].String()
		interval := 5 * time.Second
		if len(args) > 1 && args[1].Int() > 0 {
			interval = time.Duration(args[1].Int()) * time.Second
		}

		rep := sdk.NewReplicator(db, serverUrl)
		if len(args) > 2 && args[2].Type() == js.TypeString {
			rep.SetAuthSecret([]byte(args[2].String()))
		}

		liveCtx, cancel := context.WithCancel(ctx)
		stop := rep.Live(liveCtx, sdk.LiveConfig{
			Interval:   interval,
			PushEvery:  1,
			BackoffMax: time.Minute,
		})

		handle := nextRepHandle
		nextRepHandle++
		replicators[handle] = rep
		repStopFuncs[handle] = func() {
			cancel()
			stop()
			delete(replicators, handle)
		}

		// Fire onConnect hook if registered.
		if hook := js.Global().Get("nellOnConnect"); hook.Type() == js.TypeFunction {
			hook.Invoke(serverUrl)
		}

		return js.ValueOf(handle)
	}))

	// ── Live sync (WebSocket) ─────────────────────────────────────────────
	js.Global().Set("nellLiveWS", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(-1)
		}
		serverUrl := args[0].String()

		rep := sdk.NewReplicator(db, serverUrl)
		if len(args) > 1 && args[1].Type() == js.TypeString {
			rep.SetAuthSecret([]byte(args[1].String()))
		}

		liveCtx, cancel := context.WithCancel(ctx)
		stop := rep.LiveWS(liveCtx, store.NodeID())

		handle := nextRepHandle
		nextRepHandle++
		replicators[handle] = rep
		repStopFuncs[handle] = func() {
			cancel()
			stop()
			delete(replicators, handle)
		}

		if hook := js.Global().Get("nellOnConnect"); hook.Type() == js.TypeFunction {
			hook.Invoke(serverUrl)
		}

		return js.ValueOf(handle)
	}))

	// ── Stop sync ─────────────────────────────────────────────────────────
	js.Global().Set("nellStopSync", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.Undefined()
		}
		handle := args[0].Int()
		if stop, ok := repStopFuncs[handle]; ok {
			stop()
			delete(repStopFuncs, handle)
			if hook := js.Global().Get("nellOnDisconnect"); hook.Type() == js.TypeFunction {
				hook.Invoke()
			}
		}
		return js.Undefined()
	}))

	// ── Set auth secret ───────────────────────────────────────────────────
	// Sets the HMAC secret for subsequent sync calls (nellSync, nellLiveSync,
	// nellLiveWS).  Pass an empty string to disable auth.
	js.Global().Set("nellSetAuth", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.Undefined()
		}
		secret := args[0].String()
		authSecret = []byte(secret)
		return js.Undefined()
	}))

	// ── Destroy ───────────────────────────────────────────────────────────
	js.Global().Set("nellDestroy", js.FuncOf(func(this js.Value, args []js.Value) any {
		return asyncPromise(func() (any, error) {
			if err := db.Destroy(ctx); err != nil {
				return errorJSON(err.Error()), nil
			}
			resp, _ := json.Marshal(map[string]any{"ok": true})
			return string(resp), nil
		})
	}))

	js.Global().Set("nellReady", js.ValueOf(true))
	js.Global().Set("nellNodeID", js.FuncOf(func(this js.Value, args []js.Value) any {
		return js.ValueOf(store.NodeID())
	}))

	js.Global().Set("nellInfo", js.FuncOf(func(this js.Value, args []js.Value) any {
		info := db.Info()
		resp, _ := json.Marshal(info)
		return js.ValueOf(string(resp))
	}))
}

func errorJSON(msg string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error": msg})
	return string(b)
}

func main() {
	ch := make(chan struct{}, 0)

	var err error
	store, err = nell.NewIndexedDBStore()
	if err != nil {
		fmt.Println("Falling back to MemoryStore:", err)
		fallbackID, uuidErr := nell.GenerateUUIDv4()
		if uuidErr != nil {
			fmt.Println("Falling back to time-based nodeID:", uuidErr)
			fallbackID = fmt.Sprintf("wasm-fallback-%d", time.Now().UnixNano())
		}
		store = nell.NewMemoryStore(fallbackID)
	}
	db = sdk.New(store, store.NodeID(), nell.DefaultCollection)

	registerCallbacks()
	<-ch
}
