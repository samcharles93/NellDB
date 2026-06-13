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
