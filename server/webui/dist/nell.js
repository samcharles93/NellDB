/**
 * NellDB — JavaScript SDK
 *
 * Distributed, real-time, offline-first document database.
 * One import, one init call, then full CRUD + sync.
 *
 * @example
 *   import { NellDB } from '@nelldb/sdk';
 *   const db = new NellDB();
 *   await db.init();
 *   await db.put({ _id: 'note-1', title: 'Hello' });
 *   await db.replicate.to('https://home.example.com');
 */
const isNode = typeof window === 'undefined';
const globalScope = isNode ? global : window;

if (!globalScope.Go && !isNode) {
    // wasm_exec.js must be loaded before this file in browser contexts.
    // In Node, require it dynamically.
}

class NellDB {
    constructor() {
        this.go = new globalScope.Go();
        this.instance = null;
    }

    // ── Lifecycle ─────────────────────────────────────────────────────────

    /**
     * Load the WASM engine.  Must be called once before any operations.
     * @param {string|ArrayBuffer|Uint8Array} wasmUrlOrBuffer
     */
    async init(wasmUrlOrBuffer) {
        if (this.instance) return;

        if (isNode || wasmUrlOrBuffer instanceof ArrayBuffer || wasmUrlOrBuffer instanceof Uint8Array) {
            const result = await WebAssembly.instantiate(wasmUrlOrBuffer, this.go.importObject);
            this.instance = result.instance;
        } else {
            const result = await WebAssembly.instantiateStreaming(fetch(wasmUrlOrBuffer), this.go.importObject);
            this.instance = result.instance;
        }

        this.go.run(this.instance);
    }

    // ── CRUD ──────────────────────────────────────────────────────────────

    /**
     * Insert or update a document.
     * @param {object} doc - Document object, requires an _id.
     * @returns {Promise<{ok:boolean, rev:string}>}
     */
    async put(doc) {
        this._requireReady();
        const respRaw = await globalScope.nellPut(JSON.stringify(doc));
        const resp = JSON.parse(respRaw);
        if (!resp.ok) throw new Error(resp.error);
        // Note: the SDK docdb mutates the passed in doc with _rev in Go,
        // we reflect that here if we want or just return the rev.
        doc._rev = resp.rev;
        return resp;
    }

    /**
     * Fetch a single document by ID.
     * @param {string} id
     * @returns {Promise<object>} The document.
     */
    async get(id) {
        this._requireReady();
        const respRaw = await globalScope.nellGet(id);
        const resp = JSON.parse(respRaw);
        if (!resp.ok) throw new Error(resp.error);
        return resp.doc;
    }

    /**
     * Tombstone a document.
     * @param {string|object} idOrDoc
     * @returns {Promise<{ok:boolean, rev:string}>}
     */
    async remove(idOrDoc) {
        this._requireReady();
        const arg = typeof idOrDoc === 'string' ? idOrDoc : JSON.stringify(idOrDoc);
        const respRaw = await globalScope.nellRemove(arg);
        const resp = JSON.parse(respRaw);
        if (!resp.ok) throw new Error(resp.error);
        return resp;
    }

    /**
     * List all non-deleted documents.
     * @param {object} options
     * @returns {Promise<object>} AllDocsResult
     */
    async allDocs(options = {}) {
        this._requireReady();
        const respRaw = await globalScope.nellAllDocs(JSON.stringify(options));
        const resp = JSON.parse(respRaw);
        if (!resp.ok) throw new Error(resp.error);
        return resp.result;
    }

    /**
     * Get database metadata and statistics.
     * @returns {Promise<object>}
     */
    async info() {
        this._requireReady();
        const infoRaw = await globalScope.nellInfo();
        return JSON.parse(infoRaw);
    }

    /**
     * Get the local node identifier.
     * @returns {string}
     */
    nodeID() {
        this._requireReady();
        return globalScope.nellNodeID();
    }

    // ── Sync ──────────────────────────────────────────────────────────────

    /**
     * Connect to a home server and begin syncing.
     * @param {string} serverUrl - e.g. "https://home.example.com"
     * @returns {Promise<{ok:boolean, pushed:number, pulled:number}>}
     */
    async sync(serverUrl) {
        this._requireReady();
        this._serverUrl = serverUrl;
        
        const respRaw = await globalScope.nellSync(serverUrl);
        const resp = JSON.parse(respRaw);
        if (!resp.ok) throw new Error(resp.error);

        if (this._onSyncComplete) this._onSyncComplete();
        return resp;
    }

    // ── Lifecycle hooks ───────────────────────────────────────────────────

    /** @param {() => void} cb */
    onConnect(cb) { this._onConnect = cb; }

    /** @param {() => void} cb */
    onDisconnect(cb) { this._onDisconnect = cb; }

    /** @param {(id:string, local:object, accepted:object) => void} cb */
    onConflict(cb) { this._onConflict = cb; }

    /** @param {() => void} cb */
    onSyncComplete(cb) { this._onSyncComplete = cb; }

    // ── Internal ──────────────────────────────────────────────────────────

    _requireReady() {
        if (!globalScope.nellPut) {
            throw new Error('Nell WASM engine not initialised. Call await db.init() first.');
        }
    }
}

module.exports = { NellDB };
