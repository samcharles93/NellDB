//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/samcharles93/NellDB"
	"github.com/samcharles93/NellDB/sdk"
)

var (
	store nell.Store
	db    *sdk.DocDB
	ctx   = context.Background()
)

func registerCallbacks() {
	// ── Write ──────────────────────────────────────────────────────────────
	js.Global().Set("nellPut", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(errorJSON("missing argument"))
		}
		var doc sdk.Doc
		if err := json.Unmarshal([]byte(args[0].String()), &doc); err != nil {
			return js.ValueOf(errorJSON(err.Error()))
		}
		rev, err := db.Put(ctx, doc)
		if err != nil {
			return js.ValueOf(errorJSON(err.Error()))
		}
		resp, _ := json.Marshal(map[string]any{
			"ok":  true,
			"rev": rev,
		})
		return js.ValueOf(string(resp))
	}))

	// ── Read ──────────────────────────────────────────────────────────────
	js.Global().Set("nellGet", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(errorJSON("missing id"))
		}
		doc, err := db.Get(ctx, args[0].String())
		if err != nil {
			return js.ValueOf(errorJSON(err.Error()))
		}
		resp, _ := json.Marshal(map[string]any{"ok": true, "doc": doc})
		return js.ValueOf(string(resp))
	}))

	// ── Remove ────────────────────────────────────────────────────────────
	js.Global().Set("nellRemove", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(errorJSON("missing argument"))
		}

		var idOrDoc any
		argStr := args[0].String()
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
			return js.ValueOf(errorJSON(err.Error()))
		}
		resp, _ := json.Marshal(map[string]any{"ok": true, "rev": rev})
		return js.ValueOf(string(resp))
	}))

	// ── AllDocs ───────────────────────────────────────────────────────────
	js.Global().Set("nellAllDocs", js.FuncOf(func(this js.Value, args []js.Value) any {
		var rng sdk.DocRange
		if len(args) > 0 && args[0].Type() == js.TypeString && args[0].String() != "" {
			if err := json.Unmarshal([]byte(args[0].String()), &rng); err != nil {
				return js.ValueOf(errorJSON(err.Error()))
			}
		}
		res, err := db.AllDocs(ctx, rng)
		if err != nil {
			return js.ValueOf(errorJSON(err.Error()))
		}
		resp, _ := json.Marshal(map[string]any{"ok": true, "result": res})
		return js.ValueOf(string(resp))
	}))

	// ── Sync ──────────────────────────────────────────────────────────────
	js.Global().Set("nellSync", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.ValueOf(errorJSON("missing serverUrl"))
		}
		serverUrl := args[0].String()

		// Wrap async work in a JS Promise since sync hits the network
		promiseConstructor := js.Global().Get("Promise")
		return promiseConstructor.New(js.FuncOf(func(this js.Value, pArgs []js.Value) any {
			resolve := pArgs[0]
			reject := pArgs[1]

			go func() {
				rep := sdk.NewReplicator(db, serverUrl)
				pushed, pulled, err := rep.Sync(ctx)
				if err != nil {
					reject.Invoke(js.Global().Get("Error").New(err.Error()))
					return
				}
				resp, _ := json.Marshal(map[string]any{
					"ok":     true,
					"pushed": pushed,
					"pulled": pulled,
				})
				resolve.Invoke(js.ValueOf(string(resp)))
			}()
			return nil
		}))
	}))

	js.Global().Set("nellReady", js.ValueOf(true))
}

func errorJSON(msg string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error": msg})
	return string(b)
}

func main() {
	ch := make(chan struct{}, 0)

	var err error
	store, err = nell.NewIndexedDBStore("wasm-client")
	if err != nil {
		fmt.Println("Falling back to MemoryStore:", err)
		store = nell.NewMemoryStore("wasm-client")
	}
	db = sdk.New(store, "wasm-client")

	registerCallbacks()
	<-ch
}
