// Run with: node --test tests/live-matrix.test.cjs
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const {join} = require('node:path');
const {test} = require('node:test');
const vm = require('node:vm');

function createPage(storage = new Map(), username = 'viewer', source = 'local') {
    const nodes = new Map();
    const plays = [];
    const events = new Map();
    const run = code => vm.runInContext(code, context);
    const context = vm.createContext({
        window: {
            innerWidth: 1200,
            CAMKEEP_AUTH: {user: {username, source, role: 'viewer'}},
            addEventListener(name, callback) {
                if (!events.has(name)) events.set(name, []);
                events.get(name).push(callback);
            }
        },
        document: {
            readyState: 'loading',
            addEventListener() {},
            getElementById(id) {
                if (!nodes.has(id)) nodes.set(id, {
                    classList: {add() {}, remove() {}, toggle() {}},
                    insertAdjacentHTML() {},
                    pause() {}, load() {}, removeAttribute() {}
                });
                return nodes.get(id);
            }
        },
        localStorage: {
            getItem: key => storage.get(key) || null,
            setItem: (key, value) => storage.set(key, value)
        },
        console: {error() {}, warn() {}},
        setInterval() {},
        recordPlay: (...args) => plays.push(args)
    });
    for (const file of ['index.js', 'camera-nodes.js']) {
        vm.runInContext(readFileSync(join(__dirname, '../static/js', file), 'utf8'), context, {filename: file});
    }
    // Keep matrix state, rendering, persistence, and status loading real; stub unrelated UI and media transport.
    for (const name of [
        'initThemeControls', 'initAccountMenu', 'initUpdateBadge', 'initConfigDirtyTracking',
        'initRecordRangeControls', 'renderRecordSelectionPrompt', 'syncSelectedRecordFromActiveCell',
        'syncMobileStageDevicePicker', 'refreshOnvifEventOverlay', 'refreshPTZPanel',
        'resetEmptyState', 'clearOnvifEventOverlay', 'setSelectedRecordPath',
        'updateCameraStats', 'renderCameraListFromState', 'renderStatusLoadFailure'
    ]) run(`${name} = function () {};`);
    run('window.syncMobileStageDevicePicker = syncMobileStageDevicePicker;');
    run('executePlayInCell = recordPlay;');
    return {
        context, run, plays, events, storage,
        key: run('liveMatrixStorageKey()'),
        state: () => JSON.parse(run('JSON.stringify({layout: currentLayout, activeCell, cameras: liveMatrixCameraIds, cells: cellData})')),
        saved: () => JSON.parse(storage.get(run('liveMatrixStorageKey()'))),
        async status(payload, status = 200) {
            context.fetch = async () => ({ok: status === 200, status, json: async () => payload});
            await run('loadStatus()');
        },
        async start(payload = {}) {
            let request;
            context.DPlayer = function () {};
            context.fetch = async () => ({ok: true, json: async () => payload});
            run('loadStatus = ((original) => () => captureStatusRequest(original()))(loadStatus);');
            context.captureStatusRequest = promise => (request = promise);
            run('window.onload()');
            await request;
        }
    };
}

test('refresh restores the layout, focused window, and each live device', async () => {
    const first = createPage();
    await first.start();
    first.run("setLayout(4); playVideo('front', true, 'Front'); playVideo('garage', true, 'Garage');");
    assert.equal(first.saved().activeCell, 2);

    const refreshed = createPage(first.storage);
    await refreshed.start({front: {}, garage: {stream_state: 'offline'}});
    assert.equal(refreshed.state().layout, 4);
    assert.equal(refreshed.state().activeCell, 2);
    assert.deepEqual(refreshed.plays.map(args => args.slice(0, 3)), [[0, 'front', true], [1, 'garage', true]]);
    await refreshed.status({front: {}, garage: {}});
    assert.equal(refreshed.plays.length, 2, 'polling must not restart playback');
});

test('hidden windows survive refresh and resume when the grid expands', async () => {
    const first = createPage();
    await first.start();
    first.run("setLayout(9); setActiveCell(8); playVideo('yard', true, 'Yard'); setLayout(1);");
    const refreshed = createPage(first.storage);
    await refreshed.start({yard: {}});
    assert.equal(refreshed.plays.length, 0);
    assert.equal(refreshed.state().cells[8].camId, 'yard');
    refreshed.run('setLayout(9)');
    assert.deepEqual(refreshed.plays[0].slice(0, 3), [8, 'yard', true]);
});

test('explicitly closing a window removes its saved device, while unloading preserves the others', async () => {
    const first = createPage();
    await first.start();
    first.run("setLayout(4); playVideo('front', true, 'Front'); playVideo('garage', true, 'Garage'); clearCell(0);");
    first.events.get('pagehide')[0]();
    first.events.get('beforeunload')[0]();
    assert.deepEqual(first.saved().cameras.slice(0, 2), [null, 'garage']);
    const refreshed = createPage(first.storage);
    await refreshed.start({front: {}, garage: {}});
    assert.deepEqual(refreshed.plays.map(args => args.slice(0, 2)), [[1, 'garage']]);
});

test('record playback leaves the saved live assignment available for the next refresh', async () => {
    const first = createPage();
    await first.start();
    first.run("playVideo('front', true, 'Front'); playRecordNative(0, '/play/example.mp4', 'Record', 'example.mp4', 0);");
    assert.equal(first.saved().cameras[0], 'front');
    const refreshed = createPage(first.storage);
    await refreshed.start({front: {}});
    assert.deepEqual(refreshed.plays[0].slice(0, 3), [0, 'front', true]);
});

test('device assignments are isolated by account and account source', () => {
    const first = createPage();
    first.run("setLayout(6); playVideo('front', true, 'Front');");
    for (const page of [createPage(first.storage, 'another'), createPage(first.storage, 'viewer', 'env')]) {
        page.run('restoreLiveMatrixState()');
        assert.equal(page.state().layout, 1);
        assert.ok(page.state().cameras.every(cam => cam === null));
    }
});

test('failed status requests defer restoration, and inaccessible devices are removed after success', async () => {
    const first = createPage();
    first.run("setLayout(4); playVideo('front', true, 'Front'); playVideo('removed', true, 'Removed');");
    const refreshed = createPage(first.storage);
    refreshed.run('restoreLiveMatrixState(); setLayout(currentLayout);');
    for (const status of [401, 403, 500]) await refreshed.status({}, status);
    assert.equal(refreshed.plays.length, 0);
    assert.equal(refreshed.saved().cameras[1], 'removed');
    await refreshed.status({front: {}});
    assert.deepEqual(refreshed.plays.map(args => args.slice(0, 2)), [[0, 'front']]);
    assert.equal(refreshed.saved().cameras[1], null);
});

test('choices made before the status response take precedence over saved assignments', async () => {
    const first = createPage();
    first.run("setLayout(4); playVideo('front', true, 'Front'); playVideo('garage', true, 'Garage'); playVideo('yard', true, 'Yard');");
    const refreshed = createPage(first.storage);
    refreshed.run("restoreLiveMatrixState(); setLayout(currentLayout); clearCell(0); setActiveCell(1); playVideo('new', true, 'New'); playRecordNative(2, '/play/example.mp4', 'Record', 'example.mp4', 0);");
    await refreshed.status({front: {}, garage: {}, yard: {}, new: {}});
    const state = refreshed.state();
    assert.equal(state.cells[0], null);
    assert.equal(state.cells[1].camId, 'new');
    assert.equal(state.cells[2].isLive, false);
    assert.equal(refreshed.saved().cameras[2], 'yard');
});

test('invalid or unavailable browser storage falls back safely', async () => {
    for (const value of ['{broken', JSON.stringify({version: 99, layout: 9}), JSON.stringify({version: 1, layout: 200, activeCell: -1, cameras: 'front'})]) {
        const page = createPage();
        page.storage.set(page.key, value);
        await page.start();
        assert.equal(page.state().layout, 1);
        assert.equal(page.state().activeCell, 0);
        assert.ok(page.state().cameras.every(cam => cam === null));
    }
    const page = createPage();
    page.context.localStorage.getItem = page.context.localStorage.setItem = () => {throw new Error('Storage disabled');};
    await page.start();
    assert.doesNotThrow(() => page.run("setLayout(4); playVideo('front', true, 'Front'); clearCell(0);"));
});
