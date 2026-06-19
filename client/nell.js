/**
 * NellDB — JavaScript SDK
 *
 * Distributed, real-time, offline-first document database.
 * One import, one init call, then full CRUD + sync.
 *
 * @example
 *   import { NellDB } from './nell.js';
 *   const db = new NellDB();
 *   await db.init();
 *   await db.put({ _id: 'note-1', title: 'Hello' });
 *   await db.sync('https://home.example.com');
 *
 * // Continuous sync with real-time changes:
 *   const syncHandle = db.startSync('https://home.example.com', 5000);
 *   db.onConnect(() => console.log('connected'));
 *   db.onDisconnect(() => console.log('disconnected'));
 *   const changesHandle = db.changes((change) => console.log(change));
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
        this._syncHandles = new Map();
        this._changeHandles = new Map();
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
        doc._rev = resp.rev;
        return resp;
    }

    /**
     * Insert or update multiple documents in one batch.
     * @param {object[]} docs - Array of document objects, each requires an _id.
     * @returns {Promise<{ok:boolean, revs:string[]}>}
     */
    async putMany(docs) {
        this._requireReady();
        const respRaw = await globalScope.nellPutMany(JSON.stringify(docs));
        const resp = JSON.parse(respRaw);
        if (!resp.ok) throw new Error(resp.error);
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
     * Fetch multiple documents by ID in one call.
     * @param {string[]} ids
     * @returns {Promise<{ok:boolean, docs:Object<string,object>}>}
     */
    async getMany(ids) {
        this._requireReady();
        const respRaw = await globalScope.nellGetMany(JSON.stringify(ids));
        const resp = JSON.parse(respRaw);
        if (!resp.ok) throw new Error(resp.error);
        return resp.docs;
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
     * @param {object} options - { startKey, endKey, limit, skip, keys, includeDocs }
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
     * Perform a vector similarity search using Cosine Similarity.
     * @param {number[]} vector - The query embedding vector.
     * @param {number} limit - Maximum number of results to return.
     * @returns {Promise<object[]>} The top matching documents.
     */
    async searchSimilar(vector, limit = 10) {
        this._requireReady();
        const respRaw = await globalScope.nellSearchSimilar(JSON.stringify(vector), limit);
        const resp = JSON.parse(respRaw);
        if (!resp.ok) throw new Error(resp.error);
        return resp.docs;
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

    /**
     * Tombstone every document in the database.  Clears all in-memory
     * bookkeeping.  The underlying store is left open.
     * @returns {Promise<{ok:boolean}>}
     */
    async destroy() {
        this._requireReady();
        const respRaw = await globalScope.nellDestroy();
        const resp = JSON.parse(respRaw);
        if (!resp.ok) throw new Error(resp.error);
        return resp;
    }

    // ── Sync ──────────────────────────────────────────────────────────────

    /**
     * One-shot sync: pull then push against a server.
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

    /**
     * Start continuous HTTP polling sync.  Pushes and pulls on an interval
     * with exponential backoff on errors.  Returns a handle that can be
     * passed to stopSync().
     * @param {string} serverUrl
     * @param {number} [intervalSec=5] - Poll interval in seconds.
     * @param {string} [authSecret] - Optional HMAC secret for signed sync.
     * @returns {number} Sync handle for stopSync().
     */
    startSync(serverUrl, intervalSec, authSecret) {
        this._requireReady();
        this._serverUrl = serverUrl;
        const handle = globalScope.nellLiveSync(serverUrl, intervalSec || 5, authSecret || '');
        if (handle < 0) throw new Error('failed to start sync');
        this._syncHandles.set(handle, serverUrl);
        return handle;
    }

    /**
     * Start continuous WebSocket sync.  Receives changes in real time
     * with automatic reconnect and backoff.  Returns a handle for stopSync().
     * @param {string} serverUrl
     * @param {string} [authSecret] - Optional HMAC secret.
     * @returns {number} Sync handle for stopSync().
     */
    startSyncWS(serverUrl, authSecret) {
        this._requireReady();
        this._serverUrl = serverUrl;
        const handle = globalScope.nellLiveWS(serverUrl, authSecret || '');
        if (handle < 0) throw new Error('failed to start WebSocket sync');
        this._syncHandles.set(handle, serverUrl);
        return handle;
    }

    /**
     * Stop a running sync loop (HTTP or WebSocket).
     * @param {number} handle - The handle returned by startSync/startSyncWS.
     */
    stopSync(handle) {
        this._requireReady();
        globalScope.nellStopSync(handle);
        this._syncHandles.delete(handle);
    }

    /**
     * Set the HMAC auth secret for all subsequent sync calls.
     * Pass an empty string to disable auth.
     * @param {string} secret
     */
    setAuth(secret) {
        this._requireReady();
        globalScope.nellSetAuth(secret);
    }

    // ── Changes feed ──────────────────────────────────────────────────────

    /**
     * Subscribe to local and remote document changes.
     * @param {(change:{id:string, rev:string, deleted:boolean, doc?:object}) => void} callback
     * @returns {number} Changes handle for stopChanges().
     */
    changes(callback) {
        this._requireReady();
        const handle = globalScope.nellChanges((data) => {
            callback(JSON.parse(data));
        });
        this._changeHandles.set(handle, true);
        return handle;
    }

    /**
     * Stop a changes feed subscription.
     * @param {number} handle - The handle returned by changes().
     */
    stopChanges(handle) {
        this._requireReady();
        globalScope.nellStopChanges(handle);
        this._changeHandles.delete(handle);
    }

    // ── Lifecycle hooks ───────────────────────────────────────────────────

    /**
     * Called when a sync connection is established (startSync/startSyncWS).
     * @param {() => void} cb
     */
    onConnect(cb) { this._onConnect = cb; }

    /**
     * Called when a sync connection is lost (stopSync or WebSocket disconnect).
     * @param {() => void} cb
     */
    onDisconnect(cb) { this._onDisconnect = cb; }

    /**
     * Called when an LWW conflict is detected during sync.
     * @param {(id:string, local:object, accepted:object) => void} cb
     */
    onConflict(cb) { this._onConflict = cb; }

    /**
     * Called when a one-shot sync cycle completes.
     * @param {() => void} cb
     */
    onSyncComplete(cb) { this._onSyncComplete = cb; }

    // ── Internal ──────────────────────────────────────────────────────────

    _requireReady() {
        if (!globalScope.nellPut) {
            throw new Error('Nell WASM engine not initialised. Call await db.init() first.');
        }
    }
}

module.exports = { NellDB };