// LANHub Go hub: page behaviour. Alpine.js does the binding; these are the
// components and helpers the templates call.

function humanSize(bytes) {
    // Same as the PHP helper human_filesize().
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    bytes = Math.max(Number(bytes) || 0, 0);
    let power = bytes > 0 ? Math.floor(Math.log(bytes) / Math.log(1024)) : 0;
    power = Math.min(power, units.length - 1);
    const v = bytes / Math.pow(1024, power);
    return (power === 0 ? Math.round(v) : Math.round(v * 10) / 10) + ' ' + units[power];
}

function ucfirst(s) { s = String(s || ''); return s.charAt(0).toUpperCase() + s.slice(1); }

function csrfToken() {
    const m = document.querySelector('meta[name=csrf-token]');
    return m ? m.content : '';
}

function randomToken() {
    const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    const buf = new Uint8Array(48);
    crypto.getRandomValues(buf);
    return Array.from(buf, b => chars[b % chars.length]).join('');
}

// api() calls a hub JSON endpoint; throws Error(message) on failure.
async function api(method, url, body) {
    const opts = { method, headers: { 'Accept': 'application/json', 'X-CSRF-Token': csrfToken() } };
    if (body !== undefined) {
        opts.headers['Content-Type'] = 'application/json';
        opts.body = JSON.stringify(body);
    }
    const res = await fetch(url, opts);
    if (res.status === 401) { window.location = '/login'; throw new Error('Logged out'); }
    let data = null;
    try { data = await res.json(); } catch (e) { /* empty body */ }
    if (!res.ok) throw new Error((data && data.message) || ('Request failed (' + res.status + ')'));
    return data;
}

function transfersPanel() {
    return {
        jobs: [],
        dismissed: false,
        timer: null,
        start() {
            this.poll();
            this.timer = setInterval(() => this.poll(), 2000);
            window.addEventListener('lanhub:transfer-started', () => { this.dismissed = false; this.poll(); });
        },
        async poll() {
            if (document.hidden) return;
            try { this.jobs = await api('GET', '/api/transfers'); } catch (e) { /* keep the last list */ }
        },
    };
}

function dashboard(machines, serverForm) {
    return {
        machines,
        status: {},
        speeds: {},
        refreshing: false,
        pinging: {},
        form: { open: false, id: null, name: '', host: '', port: 8765, os: 'linux', color: '#ac8544', token: '', use_tls: false, notes: '', shares: {}, errors: {} },
        init() {
            for (const m of this.machines) this.status[m.id] = { status: 'checking', hostname: null, drives: [], error: null };
            if (serverForm) this.openFromServer(serverForm);
            this.refresh();
            this.pollNetwork();
            setInterval(() => this.pollNetwork(), 5000);
        },
        info(id) { return this.status[id] || { status: 'checking', drives: [] }; },
        async refresh() {
            this.refreshing = true;
            try { Object.assign(this.status, await api('GET', '/api/dashboard/status')); } catch (e) { /* leave as is */ }
            this.refreshing = false;
        },
        async ping(id) {
            this.pinging[id] = true;
            try { Object.assign(this.status, await api('GET', '/api/dashboard/status?machine=' + id)); } catch (e) { }
            this.pinging[id] = false;
        },
        async pollNetwork() {
            if (document.hidden) return;
            try { this.speeds = await api('GET', '/api/dashboard/netstats'); } catch (e) { }
        },
        gauges(id) {
            const s = this.speeds[id] || { up: 0, down: 0, avg: 0 };
            // 1 MB/s floor keeps idle gauges resting near zero.
            const max = Math.max(s.up, s.down, s.avg, 1024 * 1024) * 1.15;
            return [
                { label: 'Upstream', value: s.up, pct: s.up / max * 100 },
                { label: 'Downstream', value: s.down, pct: s.down / max * 100 },
                { label: 'Average', value: s.avg, pct: s.avg / max * 100 },
            ];
        },
        usedPct(d) { return d.total > 0 ? Math.round((d.total - d.free) / d.total * 100) : 0; },
        pillClass(st) {
            return st === 'online' ? 'bg-green-100 text-green-700' : st === 'offline' ? 'bg-red-100 text-red-700' : 'bg-stone-100 text-stone-500';
        },
        dotClass(st) { return st === 'online' ? 'bg-green-500' : st === 'offline' ? 'bg-red-500' : 'bg-stone-400'; },
        openCreate() {
            const shares = {};
            document.querySelectorAll('select[name^=share_]').forEach(el => shares[el.name.slice(6)] = 'none');
            this.form = { open: true, id: null, name: '', host: '', port: 8765, os: 'linux', color: '#ac8544', token: '', use_tls: false, notes: '', shares, errors: {} };
        },
        openEdit(id) {
            const m = this.machines.find(x => x.id === id);
            if (!m) return;
            const shares = {};
            document.querySelectorAll('select[name^=share_]').forEach(el => {
                const uid = el.name.slice(6);
                shares[uid] = m.shares[uid] || 'none';
            });
            this.form = { open: true, id, name: m.name, host: m.host, port: m.port, os: m.os, color: m.color, token: '', use_tls: m.use_tls, notes: m.notes, shares, errors: {} };
        },
        openFromServer(f) {
            const shares = {};
            for (const k in (f.Shares || {})) shares[k] = f.Shares[k];
            this.form = { open: true, id: f.ID || null, name: f.Name, host: f.Host, port: f.Port || 8765, os: f.OS, color: f.Color, token: '', use_tls: f.UseTLS, notes: f.Notes, shares, errors: f.Errors || {} };
        },
    };
}

// ---- Explorer ----

const FILE_ICONS = {
    folder: ['text-tan-500', 'M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z'],
    image: ['text-emerald-500', 'M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14M4 8h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z'],
    video: ['text-purple-500', 'M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z'],
    audio: ['text-pink-500', 'M9 19V6l10-2v13M9 19a2 2 0 11-4 0 2 2 0 014 0zm10-2a2 2 0 11-4 0 2 2 0 014 0z'],
    archive: ['text-amber-600', 'M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4'],
    pdf: ['text-red-500', 'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z'],
    document: ['text-stone-500', 'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z'],
    code: ['text-sky-600', 'M10 20l4-16m4 4l4 4-4 4M6 8l-4 4 4 4'],
    file: ['text-stone-400', 'M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z'],
};

// One card on the Agents page: shown at once from the database, then
// filled in when this machine's agent answers (or doesn't).
function agentCard(id, lastSeen) {
    return {
        status: 'checking', lastSeen: lastSeen || '', version: '', direct: false,
        updateTo: '', canPush: false, note: '', updating: false, message: '', failed: false,
        async init() {
            await this.load();
        },
        async load() {
            const wasBehind = this.updateTo && this.canPush;
            try {
                const r = await api('GET', '/api/agents/' + id + '/status');
                this.status = r.status;
                this.lastSeen = r.last_seen || this.lastSeen;
                this.version = r.version || '';
                this.direct = !!r.direct;
                this.updateTo = r.update_to || '';
                this.canPush = !!r.can_push;
                this.note = r.update_note || '';
            } catch (e) {
                this.status = 'offline';
            }
            // Counts for the page's "Update all agents" button.
            const behind = this.updateTo && this.canPush;
            if (behind && !wasBehind) this.$dispatch('agent-behind');
            if (!behind && wasBehind) this.$dispatch('agent-current');
        },
        // The hub sends the agent its signed update and waits for it to
        // come back as the new version (up to about a minute and a half).
        async update() {
            this.updating = true; this.message = ''; this.failed = false;
            try {
                const r = await api('POST', '/api/agents/' + id + '/update');
                this.message = r.message || 'Updated.';
            } catch (e) {
                this.failed = true;
                this.message = e.message || 'The update failed.';
            }
            this.updating = false;
            await this.load();
        },
    };
}

// The install commands on the Agents page, for this hub's own address.
function agentInstall(version) {
    const hub = window.location.origin;
    return {
        open: false, version,
        linux: 'curl -fsSL ' + hub + '/agent/install.sh | sudo bash -s -- --token TOKEN',
        windows: "& ([scriptblock]::Create((irm '" + hub + "/agent/install.ps1'))) -Token 'TOKEN'",
        copy(text) { navigator.clipboard && navigator.clipboard.writeText(text); },
    };
}

function fileGroup(type, name) {
    if (type === 'dir' || type === 'drive') return 'folder';
    const ext = (String(name).split('.').pop() || '').toLowerCase();
    const groups = {
        image: ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp'], video: VIDEO_EXTS,
        audio: ['mp3', 'wav', 'flac', 'ogg'], archive: ['zip', 'rar', '7z', 'tar', 'gz'], pdf: ['pdf'],
        code: ['php', 'js', 'ts', 'py', 'rb', 'go', 'rs', 'java', 'c', 'cpp', 'json', 'yml', 'yaml', 'sh'], document: ['txt', 'md', 'doc', 'docx'],
    };
    for (const g in groups) if (String(name).includes('.') && groups[g].includes(ext)) return g;
    return 'file';
}

function fileIcon(type, name, big) {
    const [cls, d] = FILE_ICONS[fileGroup(type, name)];
    return '<svg class="' + (big ? 'w-9 h-9' : 'w-4 h-4') + ' ' + cls + ' shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">'
        + '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.75" d="' + d + '"/></svg>';
}

function isPreviewableImage(name) {
    return ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg'].includes((String(name).split('.').pop() || '').toLowerCase());
}

// Video files the LANHub player is offered for. The browser plays some of
// them itself; LANHub Media repackages or converts the rest.
const VIDEO_EXTS = ['mp4', 'm4v', 'mov', 'qt', 'webm', 'mkv', 'ogv', 'avi', 'divx', 'xvid', 'mpg', 'mpeg', 'm2v', 'vob',
    'ts', 'm2ts', 'mts', 'wmv', 'asf', 'flv', 'f4v', '3gp', '3g2', 'rm', 'rmvb', 'dv', 'mxf', 'y4m'];

// Audio types play directly (they must match mediaTypes in handlers_explorer.go).
function mediaKind(name) {
    const ext = (String(name).split('.').pop() || '').toLowerCase();
    if (VIDEO_EXTS.includes(ext)) return 'video';
    if (['mp3', 'm4a', 'aac', 'wav', 'ogg', 'oga', 'opus', 'flac'].includes(ext)) return 'audio';
    return null;
}

let paneSeq = 0;

function explorer(machines, firstId) {
    return {
        machines,
        panes: [],
        renaming: null, // { paneId, path, value }
        dragging: null,
        dropTarget: null,
        previewUrl: null,
        previewName: null,
        global: { query: '', searching: false, searched: false, results: [], failures: [], partial: false, pending: [] },

        init() {
            const order = [...this.machines].sort((a, b) => (b.id === firstId) - (a.id === firstId));
            for (const m of order) this.addPane(m.id);
        },
        machine(pane) { return this.machines.find(m => m.id === pane.machineId) || {}; },
        sep(pane) { return this.machine(pane).os === 'windows' ? '\\' : '/'; },
        root(pane) { return this.machine(pane).os === 'windows' ? 'C:\\' : '/'; },
        paneById(id) { return this.panes.find(p => p.id === id); },

        addPane(machineId) {
            const pane = { id: 'p' + (++paneSeq), machineId, path: null, entries: [], sort: 'name', dir: 'asc', showHidden: false,
                view: 'list', error: null, loading: false, uploading: null,
                search: { query: '', results: null, loading: false } };
            this.panes.push(pane);
            // Work on Alpine's reactive copy, or later changes won't redraw.
            const live = this.panes[this.panes.length - 1];
            this.load(live);
            return live;
        },
        closePane(pane) { this.panes = this.panes.filter(p => p.id !== pane.id); },

        async load(pane) {
            pane.loading = true;
            const q = pane.path === null ? '' : '?path=' + encodeURIComponent(pane.path);
            try {
                const data = await api('GET', '/api/machines/' + pane.machineId + '/list' + q);
                pane.entries = data.entries;
                pane.error = null;
            } catch (e) {
                pane.entries = [];
                pane.error = e.message;
            }
            pane.loading = false;
        },
        navigate(pane, path) { pane.path = path; pane.search.results = null; this.load(pane); },
        parentOf(pane, path) {
            const sep = this.sep(pane);
            let t = path.endsWith(sep) ? path.slice(0, -1) : path;
            const i = t.lastIndexOf(sep);
            return i > 0 ? t.slice(0, i + 1) : null;
        },
        up(pane) { if (pane.path) this.navigate(pane, this.parentOf(pane, pane.path)); },
        crumbs(pane) {
            if (pane.path === null) return [];
            const sep = this.sep(pane);
            const segs = pane.path.split(sep).filter(s => s !== '');
            let running = sep === '\\' ? '' : '/';
            return segs.map((seg, i) => {
                running = (i === 0 && sep === '\\') ? seg + sep : running + seg + sep;
                return { label: seg, path: running };
            });
        },
        visible(pane) {
            let list = pane.showHidden ? [...pane.entries] : pane.entries.filter(e => !e.hidden);
            const dir = pane.dir === 'asc' ? 1 : -1;
            const isDir = e => e.type === 'dir' || e.type === 'drive';
            list.sort((a, b) => {
                if (isDir(a) !== isDir(b)) return isDir(a) ? -1 : 1;
                if (pane.sort === 'size') return ((a.size ?? -1) - (b.size ?? -1)) * dir;
                if (pane.sort === 'modified') return String(a.modified || '').localeCompare(String(b.modified || '')) * dir;
                return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) * dir;
            });
            return list;
        },
        sortBy(pane, col) {
            if (pane.sort === col) pane.dir = pane.dir === 'asc' ? 'desc' : 'asc';
            else { pane.sort = col; pane.dir = 'asc'; }
        },

        // -- changes --
        async mkdir(pane) {
            if (pane.path === null) return;
            const sep = this.sep(pane);
            const target = pane.path.replace(/[\\/]+$/, '') + sep + 'New Folder';
            try {
                await api('POST', '/api/machines/' + pane.machineId + '/mkdir', { path: target });
                await this.load(pane);
                this.renaming = { paneId: pane.id, path: target, value: 'New Folder' };
            } catch (e) { pane.error = e.message; }
        },
        startRename(pane, entry) { this.renaming = { paneId: pane.id, path: entry.path, value: entry.name }; },
        isRenaming(pane, entry) { return this.renaming && this.renaming.paneId === pane.id && this.renaming.path === entry.path; },
        async confirmRename() {
            const r = this.renaming;
            if (!r || !r.value) { this.renaming = null; return; }
            const pane = this.paneById(r.paneId);
            this.renaming = null;
            try { await api('POST', '/api/machines/' + pane.machineId + '/rename', { path: r.path, new_name: r.value }); }
            catch (e) { pane.error = e.message; }
            this.load(pane);
        },
        async remove(pane, entry) {
            if (!confirm('Delete ' + entry.name + '?')) return;
            try { await api('POST', '/api/machines/' + pane.machineId + '/delete', { path: entry.path }); }
            catch (e) { pane.error = e.message; }
            this.load(pane);
        },
        upload(pane, files, dir) {
            dir = dir || pane.path;
            if (!files || !files.length || !dir) return;
            const fd = new FormData();
            for (const f of files) fd.append('files', f, f.name);
            const xhr = new XMLHttpRequest();
            xhr.open('POST', '/api/machines/' + pane.machineId + '/upload?path=' + encodeURIComponent(dir));
            xhr.setRequestHeader('X-CSRF-Token', csrfToken());
            xhr.setRequestHeader('Accept', 'application/json');
            pane.uploading = 0;
            xhr.upload.onprogress = e => { if (e.lengthComputable) pane.uploading = Math.round(e.loaded / e.total * 100); };
            xhr.onload = () => {
                pane.uploading = null;
                if (xhr.status >= 300) {
                    try { pane.error = JSON.parse(xhr.responseText).message; } catch (e) { pane.error = 'Upload failed (' + xhr.status + ')'; }
                }
                this.load(pane);
            };
            xhr.onerror = () => { pane.uploading = null; pane.error = 'The upload was interrupted.'; };
            xhr.send(fd);
        },

        // -- drag and drop --
        startDrag(event, pane, entry) {
            if (entry.type === 'drive') return;
            this.dragging = { paneId: pane.id, path: entry.path, name: entry.name, type: entry.type };
            event.dataTransfer.effectAllowed = 'copyMove';
            event.dataTransfer.setData('text/plain', JSON.stringify(this.dragging));
            const canvas = document.createElement('canvas');
            const ctx = canvas.getContext('2d');
            const label = entry.name.length > 28 ? entry.name.slice(0, 25) + '…' : entry.name;
            ctx.font = '600 13px system-ui, sans-serif';
            canvas.width = ctx.measureText(label).width + 28; canvas.height = 32;
            ctx.font = '600 13px system-ui, sans-serif';
            ctx.fillStyle = '#ac8544'; ctx.beginPath(); ctx.roundRect(0, 0, canvas.width, canvas.height, 16); ctx.fill();
            ctx.fillStyle = '#ffffff'; ctx.textBaseline = 'middle'; ctx.fillText(label, 14, 17);
            event.dataTransfer.setDragImage(canvas, 12, 16);
        },
        endDrag() { this.dragging = null; this.dropTarget = null; },
        async handleDrop(event, pane, targetPath) {
            this.dropTarget = null;
            if (event.dataTransfer.files && event.dataTransfer.files.length > 0) {
                this.upload(pane, event.dataTransfer.files, targetPath || pane.path);
                return;
            }
            const raw = event.dataTransfer.getData('text/plain');
            if (!raw) return;
            let data;
            try { data = JSON.parse(raw); } catch (e) { return; }
            const src = this.paneById(data.paneId);
            if (!src || (data.paneId === pane.id && data.path === targetPath)) return;
            if (!targetPath && !pane.path) {
                pane.error = 'Open a folder on ' + this.machine(pane).name + ' first, or drop onto one of its drives or folders.';
                return;
            }
            let failure = null;
            try {
                const res = await api('POST', '/api/drop', {
                    source_machine: src.machineId, source_path: data.path, name: data.name, target_machine: pane.machineId,
                    target_dir: targetPath ?? pane.path, copy: !event.ctrlKey, is_dir: data.type === 'dir',
                });
                if (res && res.result === 'relay') window.dispatchEvent(new Event('lanhub:transfer-started'));
            } catch (e) { failure = e.message; }
            // Reload first, then show any error: a reload clears the pane's
            // error, which made a refused drop flash red and vanish.
            await Promise.all([this.load(src), src !== pane ? this.load(pane) : null]);
            if (failure) pane.error = failure;
        },

        // -- preview / download --
        previewKind: 'frame',
        // Images fit the window, video and audio use the browser's player,
        // everything else (PDF, text) opens in the sandboxed frame.
        previewVideo: null, // { machineId, path } while the LANHub player is open
        openPreview(pane, entry) {
            this.previewKind = isPreviewableImage(entry.name) ? 'image' : (mediaKind(entry.name) || 'frame');
            this.previewName = entry.name;
            if (this.previewKind === 'video') {
                // The LANHub player works out how to play it (see lanhubPlayer).
                this.previewVideo = { machineId: pane.machineId, path: entry.path };
                this.previewUrl = 'player';
                return;
            }
            const endpoint = this.previewKind === 'audio' ? '/media' : '/preview';
            this.previewUrl = '/machines/' + pane.machineId + endpoint + '?path=' + encodeURIComponent(entry.path);
        },
        closePreview() {
            // Stop playback (and any conversion behind it) before hiding.
            document.querySelectorAll('[data-preview-media]').forEach(el => { el.pause(); el.removeAttribute('src'); el.load(); });
            this.previewUrl = null; this.previewName = null; this.previewVideo = null;
        },
        downloadUrl(pane, entry) { return '/machines/' + pane.machineId + '/download?path=' + encodeURIComponent(entry.path); },
        thumbUrl(pane, entry) { return '/machines/' + pane.machineId + '/preview?path=' + encodeURIComponent(entry.path); },

        // -- search --
        async searchPane(pane) {
            const q = pane.search.query.trim();
            if (!q) { pane.search.results = null; return; }
            pane.search.loading = true;
            try {
                const res = await api('GET', '/api/machines/' + pane.machineId + '/search?q=' + encodeURIComponent(q)
                    + '&path=' + encodeURIComponent(pane.path || this.root(pane)));
                pane.search.results = res.entries;
                pane.error = res.truncated ? 'Showing partial results — the search hit its limit.' : null;
            } catch (e) { pane.search.results = []; pane.error = e.message; }
            pane.search.loading = false;
        },
        clearSearch(pane) { pane.search.query = ''; pane.search.results = null; },
        openSearchResult(pane, r) {
            this.clearSearch(pane);
            this.navigate(pane, r.type === 'dir' ? r.path : this.parentOf(pane, r.path));
        },

        // Search everywhere: results stream in as each machine answers.
        async runGlobalSearch() {
            const q = this.global.query.trim();
            if (!q) { this.clearGlobal(); return; }
            this.global = { query: q, searching: true, searched: true, results: [], failures: [], partial: false,
                pending: this.machines.map(m => m.name) };
            try {
                const res = await fetch('/api/search?q=' + encodeURIComponent(q), { headers: { 'Accept': 'application/x-ndjson' } });
                if (!res.ok) throw new Error('Search failed (' + res.status + ')');
                const reader = res.body.getReader();
                const decoder = new TextDecoder();
                let buf = '';
                for (;;) {
                    const { value, done } = await reader.read();
                    if (done) break;
                    buf += decoder.decode(value, { stream: true });
                    let nl;
                    while ((nl = buf.indexOf('\n')) >= 0) {
                        const line = buf.slice(0, nl).trim();
                        buf = buf.slice(nl + 1);
                        if (line) this.onSearchEvent(JSON.parse(line));
                    }
                }
            } catch (e) {
                this.global.failures.push({ machine: 'Search', color: '#8c7a63', reason: e.message });
            }
            this.global.searching = false;
            this.global.pending = [];
        },
        onSearchEvent(ev) {
            const g = this.global;
            if (ev.type === 'results') {
                for (const e of ev.entries || []) g.results.push({ ...e, machine_id: ev.machine_id, machine: ev.machine, color: ev.color });
                if (ev.truncated) g.partial = true;
            } else if (ev.type === 'failed') {
                g.failures.push({ machine: ev.machine, color: ev.color, reason: ev.reason });
            } else if (ev.type === 'machine_done') {
                g.pending = g.pending.filter(n => n !== ev.machine);
            }
        },
        clearGlobal() { this.global = { query: '', searching: false, searched: false, results: [], failures: [], partial: false, pending: [] }; },
        openGlobalResult(r) {
            let pane = this.panes.find(p => p.machineId === r.machine_id);
            if (!pane) pane = this.addPane(r.machine_id);
            this.navigate(pane, this.parentOf(pane, r.path));
        },
    };
}

// ---- Sync rules and users forms ----

function ruleForm(server) {
    const blank = { id: null, name: '', src: '', srcPath: '', dst: '', dstPath: '', direction: 'one_way', interval: 60, enabled: true };
    return {
        open: !!server,
        f: server ? { id: server.ID || null, name: server.Name, src: String(server.SrcID || ''), srcPath: server.SrcPath, dst: String(server.DstID || ''),
            dstPath: server.DstPath, direction: server.Direction || 'one_way', interval: server.Interval, enabled: server.Enabled } : { ...blank },
        errors: (server && server.Errors) || {},
        openCreate() { this.f = { ...blank }; this.errors = {}; this.open = true; },
        openEdit(r) {
            this.f = { id: r.ID, name: r.Name, src: String(r.SrcID), srcPath: r.SrcPath, dst: String(r.DstID), dstPath: r.DstPath,
                direction: r.Direction, interval: r.Interval, enabled: r.Enabled };
            this.errors = {};
            this.open = true;
        },
    };
}

function inviteForm(server, machines) {
    return {
        machines: machines || [],
        open: !!server,
        name: server ? server.Name : '',
        email: server ? server.Email : '',
        roles: server && server.Roles ? { ...server.Roles } : {},
        errors: (server && server.Errors) || {},
        openInvite() { this.name = ''; this.email = ''; this.roles = {}; this.errors = {}; this.open = true; },
        // A machine has one role: ticking one column clears the other.
        setRole(id, role) { this.roles[id] = this.roles[id] === role ? 'none' : role; },
        all(role) { return this.machines.length > 0 && this.machines.every(m => this.roles[m.ID] === role); },
        toggleAll(role) {
            const set = !this.all(role);
            for (const m of this.machines) {
                if (set) this.roles[m.ID] = role;
                else if (this.roles[m.ID] === role) this.roles[m.ID] = 'none';
            }
        },
    };
}


// ---- LANHub player ----
// Plays a video in the preview window. Asks the hub how: "direct" plays the
// file as it is; "remux", "audio" and "transcode" stream a version LANHub
// Media makes on the fly. A converted stream has no length of its own, so the
// player shows the file's real length and seeks by restarting the stream at
// the new point.

function fmtTime(t) {
    t = Math.max(0, Math.floor(t || 0));
    const h = Math.floor(t / 3600), m = Math.floor(t / 60) % 60, s = t % 60;
    return (h ? h + ':' + String(m).padStart(2, '0') : m) + ':' + String(s).padStart(2, '0');
}

function lanhubPlayer(machineId, path) {
    return {
        plan: null, error: null, loading: true, playing: false, muted: false, volume: 1,
        start: 0, position: 0, duration: 0, dragging: false, waiting: false,
        get converted() { return this.plan && ['remux', 'audio', 'transcode'].includes(this.plan.mode); },
        get label() {
            if (!this.plan) return '';
            return { direct: 'Original', remux: 'Repackaged by LANHub Media', audio: 'Sound converted by LANHub Media',
                transcode: 'Converted by LANHub Media' + (this.plan.hw ? ' on the GPU' : '') }[this.plan.mode] || '';
        },
        async init() {
            try {
                this.plan = await api('GET', '/api/machines/' + machineId + '/media-info?path=' + encodeURIComponent(path));
            } catch (e) { this.error = e.message; this.loading = false; return; }
            if (this.plan.mode === 'unplayable' || this.plan.mode === 'audio_only') {
                this.error = this.plan.reason; this.loading = false; return;
            }
            this.duration = this.plan.duration || 0;
            this.load(0);
        },
        url(start) {
            if (!this.converted) return '/machines/' + machineId + '/media?path=' + encodeURIComponent(path);
            return '/machines/' + machineId + '/play?' + new URLSearchParams({ path, mode: this.plan.mode, audio: this.plan.audio || '', start: start.toFixed(2) });
        },
        load(start) {
            const v = this.$refs.video;
            this.start = this.converted ? start : 0;
            this.loading = true;
            v.src = this.url(start);
            if (!this.converted && start > 0) v.currentTime = start;
            v.play().catch(() => {});
        },
        onTime() {
            const v = this.$refs.video;
            if (!this.dragging) this.position = this.start + v.currentTime;
            if (!this.converted && isFinite(v.duration)) this.duration = v.duration;
        },
        onError() {
            const v = this.$refs.video;
            if (!v.getAttribute('src')) return;
            this.loading = false;
            this.error = this.converted
                ? 'LANHub Media couldn\'t stream this video (it may be busy with another one). Try again in a moment.'
                : 'The browser can\'t play this video.';
        },
        toggle() { const v = this.$refs.video; v.paused ? v.play() : v.pause(); },
        seek(t) {
            t = Math.min(Math.max(0, Number(t)), Math.max(0, this.duration - 1));
            this.dragging = false;
            if (this.converted) { this.position = t; this.load(t); }
            else this.$refs.video.currentTime = t;
        },
        setVolume(x) { const v = this.$refs.video; v.volume = this.volume = Number(x); v.muted = this.muted = this.volume === 0; },
        toggleMute() { const v = this.$refs.video; v.muted = this.muted = !this.muted; },
        fullscreen() { const el = this.$root; document.fullscreenElement ? document.exitFullscreen() : el.requestFullscreen && el.requestFullscreen(); },
    };
}

// Settings → Updates: follows an update while it downloads, while the hub
// restarts (requests fail for a few seconds), and until the new version
// answers, then reloads the page.
function updateProgress(initial) {
    return {
        phase: initial.phase || '', detail: initial.detail || '', target: initial.target || '',
        bytes: initial.bytes || 0, version: initial.version || '', done: false, timer: null, misses: 0,
        get busy() { return this.phase === 'downloading' || this.phase === 'restarting'; },
        get failed() { return this.phase === 'failed'; },
        init() { if (this.busy) this.poll(); },
        headline() {
            if (this.done) return 'Updated to ' + this.target + '. Reloading…';
            if (this.failed) return 'The update didn\'t finish';
            if (this.phase === 'restarting') return 'Restarting LANHub…';
            return 'Updating to ' + this.target + '…';
        },
        poll() {
            clearTimeout(this.timer);
            this.timer = setTimeout(async () => {
                try {
                    const r = await fetch('/api/updates/status', { headers: { Accept: 'application/json' }, cache: 'no-store' });
                    if (!r.ok) throw new Error(r.status);
                    const s = await r.json();
                    this.misses = 0;
                    if (this.target && s.version === this.target) {
                        this.done = true; this.phase = ''; this.detail = 'LANHub ' + s.version + ' is running.';
                        setTimeout(() => location.assign('/settings?tab=updates'), 1500);
                        return;
                    }
                    Object.assign(this, { phase: s.phase, detail: s.detail, bytes: s.bytes, target: s.target || this.target });
                } catch (e) {
                    // The hub is restarting.
                    this.misses++;
                    this.phase = 'restarting';
                    this.detail = this.misses > 60 ? 'This is taking longer than expected. Reload the page in a minute.' : 'Waiting for LANHub to come back…';
                }
                if (this.busy) this.poll();
            }, 1500);
        },
    };
}
