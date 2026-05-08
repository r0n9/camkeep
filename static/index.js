// === 播放矩阵状态管理 ===
let currentLayout = 1;
let activeCell = 0;
let dpInstances = new Array(6).fill(null);
let cellData = new Array(6).fill(null);
let currentSelectedCam = null;
let pendingAction = null;
let compactGrid = window.innerWidth < 640;

window.onload = function () {
    if (typeof DPlayer === 'undefined') {
        alert("播放器组件加载失败，请检查网络！");
        return;
    }
    setLayout(1);
    loadStatus();
    setInterval(loadStatus, 5000);
    window.addEventListener('resize', () => {
        const nextCompactGrid = window.innerWidth < 640;
        if (nextCompactGrid !== compactGrid) {
            compactGrid = nextCompactGrid;
            renderGrid();
        }
    });
};

// --- 控制面板动作弹窗 ---
function confirmCamAction(camId, action) {
    pendingAction = {camId, action};
    const titleEl = document.getElementById('confirmTitle');
    const descEl = document.getElementById('confirmDesc');
    const btnEl = document.getElementById('confirmBtn');
    const iconEl = document.getElementById('confirmIcon');

    if (action === 'start') {
        titleEl.innerText = `强制开启录像 (CAM-${camId})`;
        descEl.innerHTML = `
            <p class="mb-2">此操作将<b>无视配置中的时间表</b>，立刻开始录像。</p>
            <p class="mb-2">摄像头将<b>一直保持录制状态</b>，直到您手动点击“停录”或恢复“计划”。</p>
            <p class="text-xs text-green-600 font-bold mt-3 border-t pt-2">⚡ 确认后稍后即生效</p>`;
        btnEl.className = "px-4 py-2 bg-green-600 hover:bg-green-700 text-white rounded-lg font-bold shadow transition-all";
        btnEl.innerText = "确认强录";
        iconEl.className = "w-10 h-10 rounded-full flex items-center justify-center mr-3 text-white bg-green-500 shadow-lg shadow-green-200";
        iconEl.innerHTML = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>`;
    } else if (action === 'stop') {
        titleEl.innerText = `强制停止录像 (CAM-${camId})`;
        descEl.innerHTML = `
            <p class="mb-2">此操作将<b>立即中断当前的录像任务</b>。</p>
            <p class="mb-2">摄像头将<b>一直保持不录像状态</b>（即使在计划时间内也不会录），直到您手动点击“强制录”或恢复“计划”。</p>
            <p class="text-xs text-red-500 font-bold mt-3 border-t pt-2">⚡ 确认后稍后即生效</p>`;
        btnEl.className = "px-4 py-2 bg-red-600 hover:bg-red-700 text-white rounded-lg font-bold shadow transition-all";
        btnEl.innerText = "确认停录";
        iconEl.className = "w-10 h-10 rounded-full flex items-center justify-center mr-3 text-white bg-red-500 shadow-lg shadow-red-200";
        iconEl.innerHTML = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 10a1 1 0 011-1h4a1 1 0 011 1v4a1 1 0 01-1 1h-4a1 1 0 01-1-1v-4z"></path></svg>`;
    } else if (action === 'auto') {
        titleEl.innerText = `恢复自动计划 (CAM-${camId})`;
        descEl.innerHTML = `
            <p class="mb-2">解除强制状态，将摄像头的录像控制权交还给系统。</p>
            <p class="mb-2">系统将严格按照 conf.yaml 中的 <code>record_time</code> 时间表自动启停录像。</p>
            <p class="text-xs text-blue-500 font-bold mt-3 border-t pt-2">⚡ 确认后立即应用计划逻辑</p>`;
        btnEl.className = "px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg font-bold shadow transition-all";
        btnEl.innerText = "恢复计划";
        iconEl.className = "w-10 h-10 rounded-full flex items-center justify-center mr-3 text-white bg-blue-500 shadow-lg shadow-blue-200";
        iconEl.innerHTML = `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"></path></svg>`;
    }
    document.getElementById('confirmModal').classList.remove('hidden');
}

function closeConfirm() {
    document.getElementById('confirmModal').classList.add('hidden');
    pendingAction = null;
}

async function executeConfirmAction() {
    if (!pendingAction) return;
    const {camId, action} = pendingAction;
    const btn = document.getElementById('confirmBtn');
    const originText = btn.innerText;
    btn.innerText = "执行中...";
    btn.disabled = true;

    await fetch(`/api/camera/${camId}/${action}`, {method: 'POST'});

    btn.innerText = originText;
    btn.disabled = false;
    closeConfirm();
    loadStatus();
}

// --- 系统配置相关 ---
async function openConfig() {
    const resp = await fetch('/api/config');
    document.getElementById('configYaml').value = await resp.text();
    syncMergeControlsFromYaml();
    document.getElementById('configModal').classList.remove('hidden');
}

function closeConfig() {
    document.getElementById('configModal').classList.add('hidden');
}

async function saveConfig() {
    applyMergeControlsToYaml();
    const yamlText = document.getElementById('configYaml').value;
    const resp = await fetch('/api/config', {method: 'POST', body: yamlText});
    if (resp.ok) {
        alert('配置已生效并自动重启任务！');
        closeConfig();
        loadStatus();
    } else {
        const err = await resp.json();
        alert('保存失败: ' + err.error);
    }
}

function syncMergeControlsFromYaml() {
    const yamlText = document.getElementById('configYaml').value;
    const enabledMatch = yamlText.match(/daily_merge:\s*\n(?:[ \t].*\n)*?[ \t]+enabled:\s*(true|false)/i);
    const timeMatch = yamlText.match(/daily_merge:\s*\n(?:[ \t].*\n)*?[ \t]+time:\s*["']?([0-2]\d:[0-5]\d)["']?/i);

    document.getElementById('dailyMergeEnabled').checked = enabledMatch ? enabledMatch[1].toLowerCase() === 'true' : false;
    document.getElementById('dailyMergeTime').value = timeMatch ? timeMatch[1] : '03:30';
}

function applyMergeControlsToYaml() {
    const textArea = document.getElementById('configYaml');
    const enabled = document.getElementById('dailyMergeEnabled').checked ? 'true' : 'false';
    const time = document.getElementById('dailyMergeTime').value || '03:30';
    const block = `daily_merge:\n  enabled: ${enabled}\n  time: "${time}"`;

    let content = textArea.value.trimStart();
    if (content.match(/(^|\n)daily_merge:\s*\n(?:[ \t].*\n)*/)) {
        content = content.replace(/(^|\n)daily_merge:\s*\n(?:[ \t].*\n)*/, `$1${block}\n`);
    } else {
        content = block + '\n' + content;
    }
    textArea.value = content;
}

async function scanUnmanagedStreams() {
    const listDiv = document.getElementById('unmanagedList');
    listDiv.innerHTML = '<span class="text-xs text-slate-500">正在与 go2rtc 通信并检索流...</span>';
    listDiv.classList.remove('hidden');

    try {
        const resp = await fetch('/api/go2rtc/unmanaged');
        if (!resp.ok) {
            const err = await resp.json();
            throw new Error(err.error || '请求失败');
        }
        const streams = await resp.json();

        if (!streams || streams.length === 0) {
            listDiv.innerHTML = '<span class="text-xs text-emerald-600 font-bold">🎉 所有 go2rtc 流均已接入系统，暂无新发现。</span>';
            return;
        }

        listDiv.innerHTML = '';
        streams.forEach(stream => {
            const tag = document.createElement('div');
            tag.id = `unmanaged-${stream}`;
            tag.className = 'flex items-center bg-white border border-blue-200 pl-3 pr-1 py-1 rounded-md shadow-sm';
            tag.innerHTML = `
                <span class="text-xs font-mono font-bold text-slate-700 mr-3">${stream}</span>
                <button onclick="appendStreamToConfig('${stream}')" class="text-[10px] bg-blue-50 text-blue-600 hover:bg-blue-600 hover:text-white px-2 py-1 rounded transition-colors font-bold">
                    ➕ 追加到配置
                </button>
            `;
            listDiv.appendChild(tag);
        });
    } catch (e) {
        listDiv.innerHTML = `<span class="text-xs text-red-500 font-bold">扫描失败: ${e.message}</span>`;
    }
}

function appendStreamToConfig(streamId) {
    const textArea = document.getElementById('configYaml');
    let content = textArea.value;

    let listIndent = "  ";
    let propIndent = "    ";
    const indentMatch = content.match(/^(\s+)-\s/m);
    if (indentMatch) {
        listIndent = indentMatch[1];
        propIndent = listIndent + "  ";
    }

    const newCamYaml = [`${listIndent}- id: "${streamId}"`, `${propIndent}rtsp_url: "managed_by_go2rtc"`, `${propIndent}auto_discovered: true`, `${propIndent}retention_days: 7`, `${propIndent}segment_duration: 600`, `${propIndent}format: ts`, `${propIndent}min_size_kb: 1024`, `${propIndent}record_time: "00:00-23:59"`, `${propIndent}mode: normal`].join('\n') + '\n';

    if (content.trim() === '') {
        content = 'cameras:\n';
    } else {
        if (!content.endsWith('\n')) content += '\n';
        if (!content.includes('cameras:')) content += 'cameras:\n';
    }

    textArea.value = content + newCamYaml;

    const tag = document.getElementById(`unmanaged-${streamId}`);
    if (tag) tag.remove();

    const listDiv = document.getElementById('unmanagedList');
    if (listDiv.children.length === 0) {
        listDiv.innerHTML = '<span class="text-xs text-emerald-600 font-bold">🎉 所有发现设备已追加到下方配置框，请根据需要调整参数后点击【保存并应用】。</span>';
    }

    textArea.classList.add('ring-2', 'ring-emerald-400', 'transition-all', 'duration-300');
    setTimeout(() => textArea.classList.remove('ring-2', 'ring-emerald-400'), 800);
}

// --- 状态加载 ---
async function loadStatus() {
    try {
        const resp = await fetch('/api/status');
        const data = await resp.json();
        const list = document.getElementById('camList');
        list.innerHTML = '';

        Object.entries(data).forEach(([id, cam]) => {
            const isRunning = cam.is_running;
            const isSelected = currentSelectedCam === id;
            const streamState = cam.stream_state || 'offline';
            let streamLight, streamText;

            if (streamState === 'online') {
                streamLight = 'bg-green-500 shadow-[0_0_5px_#22c55e]';
                streamText = '<span class="text-[10px] text-green-600 font-bold">流在线</span>';
            } else if (streamState === 'idle') {
                streamLight = 'bg-blue-400 shadow-[0_0_5px_#60a5fa]';
                streamText = '<span class="text-[10px] text-blue-500 font-bold">就绪待机</span>';
            } else {
                streamLight = 'bg-red-500 shadow-[0_0_5px_#ef4444]';
                streamText = '<span class="text-[10px] text-red-500 font-bold">流断线</span>';
            }

            const item = document.createElement('div');
            item.className = `p-3 rounded-xl border cursor-pointer transition-all flex flex-col group ${isSelected ? 'bg-blue-50 border-blue-400 ring-2 ring-blue-100' : 'bg-white border-gray-200 hover:border-blue-300 hover:shadow-sm'} ${isRunning ? '' : 'opacity-80'}`;
            item.onclick = () => selectCamera(id);

            item.innerHTML = `
                <div class="flex items-center justify-between mb-2">
                    <div class="flex items-center">
                        <div class="flex flex-col mr-3 space-y-1.5 border-r border-gray-100 pr-3 min-w-[65px]">
                            <div class="flex items-center" title="摄像机实时流状态">
                                <div class="w-2 h-2 rounded-full ${streamLight} mr-1.5 shrink-0"></div>
                                ${streamText}
                            </div>
                            <div class="flex items-center" title="本地录制状态">
                                <div class="w-2 h-2 rounded-full ${isRunning ? 'bg-red-500 shadow-[0_0_5px_#ef4444] animate-pulse' : 'bg-gray-300'} mr-1.5 shrink-0"></div>
                                <span class="text-[10px] ${isRunning ? 'text-gray-700' : 'text-gray-400'} font-bold">${isRunning ? '录制中' : '未录像'}</span>
                            </div>
                        </div>
                        <div class="flex flex-col">
                            <span class="font-bold text-gray-800 text-sm tracking-tight">${id}</span>
                            <span class="text-[10px] text-gray-400 uppercase">${cam.mode || 'Normal'}</span>
                        </div>
                    </div>
                    <button onclick="event.stopPropagation(); previewLive('${id}')"
                            class="w-8 h-8 flex items-center justify-center rounded bg-blue-600 hover:bg-blue-700 text-white shadow transition-colors"
                            title="主动拉流直播">▶</button>
                </div>

                <div class="flex justify-between items-center border-t border-gray-100 pt-2.5 mt-2">
                    <span class="text-[10px] font-bold text-gray-400">录制控制</span>
                    <div class="flex space-x-1.5">
                        <button onclick="event.stopPropagation(); confirmCamAction('${id}', 'start')"
                                class="group/btn flex items-center px-2 py-1 text-[11px] font-bold bg-emerald-50 text-emerald-600 border border-emerald-200 rounded-md hover:bg-emerald-500 hover:text-white hover:border-emerald-500 shadow-sm transition-all duration-200 active:scale-95">
                            <svg class="w-3 h-3 mr-1 text-emerald-500 group-hover/btn:text-white transition-colors" fill="currentColor" viewBox="0 0 24 24"><circle cx="12" cy="12" r="8"></circle></svg>
                            强录
                        </button>
                        <button onclick="event.stopPropagation(); confirmCamAction('${id}', 'stop')"
                                class="group/btn flex items-center px-2 py-1 text-[11px] font-bold bg-rose-50 text-rose-600 border border-rose-200 rounded-md hover:bg-rose-500 hover:text-white hover:border-rose-500 shadow-sm transition-all duration-200 active:scale-95">
                            <svg class="w-3 h-3 mr-1 text-rose-500 group-hover/btn:text-white transition-colors" fill="currentColor" viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="2"></rect></svg>
                            停录
                        </button>
                        <button onclick="event.stopPropagation(); confirmCamAction('${id}', 'auto')"
                                class="group/btn flex items-center px-2 py-1 text-[11px] font-bold bg-indigo-50 text-indigo-600 border border-indigo-200 rounded-md hover:bg-indigo-500 hover:text-white hover:border-indigo-500 shadow-sm transition-all duration-200 active:scale-95">
                            <svg class="w-3 h-3 mr-1 text-indigo-500 group-hover/btn:text-white transition-colors" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>
                            计划
                        </button>
                    </div>
                </div>
            `;
            list.appendChild(item);
        });
    } catch (e) {
        console.error("同步状态失败:", e);
    }
}

// --- 宫格矩阵与播放逻辑 ---
function setLayout(layoutCount) {
    currentLayout = layoutCount;
    if (activeCell >= layoutCount) activeCell = 0;

    [1, 4, 6].forEach(num => {
        const btn = document.getElementById(`btn-layout-${num}`);
        if (num === layoutCount) {
            btn.classList.add('bg-blue-600/50', 'text-white');
            btn.classList.remove('text-gray-400');
        } else {
            btn.classList.remove('bg-blue-600/50', 'text-white');
            btn.classList.add('text-gray-400');
        }
    });
    renderGrid();
}

function renderGrid() {
    const grid = document.getElementById('video-grid');
    grid.className = 'w-full flex-1 min-h-0 p-1 bg-black grid gap-1 transition-all duration-300 ' + (currentLayout === 1 ? 'grid-cols-1 grid-rows-1' : currentLayout === 4 ? 'grid-cols-2 grid-rows-2' : compactGrid ? 'grid-cols-2 grid-rows-3' : 'grid-cols-3 grid-rows-2');

    grid.innerHTML = '';

    for (let i = 0; i < currentLayout; i++) {
        const isFocused = i === activeCell;
        const cellHtml = `
            <div id="cell-${i}" onclick="setActiveCell(${i})" ondblclick="toggleCellFullscreen(${i})" class="relative w-full h-full bg-gray-900 border-[2px] transition-colors overflow-hidden group cursor-pointer ${isFocused ? 'border-blue-500 shadow-[inset_0_0_20px_rgba(59,130,246,0.3)]' : 'border-gray-800 hover:border-gray-600'}">
                <iframe id="live-iframe-${i}" class="w-full h-full border-0 hidden pointer-events-none" allow="autoplay; fullscreen; microphone; camera"></iframe>
                <div id="dplayer-${i}" class="w-full h-full hidden"></div>
                <video id="native-player-${i}" class="w-full h-full object-contain hidden bg-black" playsinline controls></video>
                <div id="empty-state-${i}" class="absolute inset-0 flex flex-col items-center justify-center text-gray-700 pointer-events-none group-hover:text-gray-500 transition-colors">
                    <svg class="w-8 h-8 mb-2 opacity-30" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                    <span class="text-xs font-bold tracking-wider uppercase opacity-50">窗口 ${i + 1}</span>
                </div>
                <div class="absolute top-2 left-2 z-10 bg-black/35 text-white/80 px-2 py-1 text-[10px] rounded backdrop-blur-sm border border-white/5 hidden pointer-events-none truncate opacity-55 transition-all duration-200 group-hover:bg-black/70 group-hover:text-white group-hover:border-white/10 group-hover:opacity-100" id="label-${i}"></div>
                <button onclick="event.stopPropagation(); clearCell(${i})" class="absolute top-2 right-2 z-20 hidden h-7 w-7 items-center justify-center rounded bg-black/65 text-white/80 border border-white/10 backdrop-blur-md opacity-0 pointer-events-none transition-all duration-200 group-hover:opacity-100 group-hover:pointer-events-auto hover:bg-red-500 hover:text-white" id="close-cell-${i}" title="关闭该窗口">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path></svg>
                </button>
            </div>
        `;
        grid.insertAdjacentHTML('beforeend', cellHtml);

        if (cellData[i]) {
            if (cellData[i].codecNotice) {
                showCodecNoticeInCell(i, cellData[i]);
            } else {
                executePlayInCell(i, cellData[i].source, cellData[i].isLive, cellData[i].title);
            }
        }
    }
    updateFocusUI();
}

function closeActiveCell() {
    clearCell(activeCell);
}

function clearCell(index) {
    stopCellPlayback(index);
    cellData[index] = null;

    const liveIframe = document.getElementById(`live-iframe-${index}`);
    const dplayerContainer = document.getElementById(`dplayer-${index}`);
    const nativePlayer = document.getElementById(`native-player-${index}`);
    const emptyState = document.getElementById(`empty-state-${index}`);
    const label = document.getElementById(`label-${index}`);
    const closeBtn = document.getElementById(`close-cell-${index}`);

    if (liveIframe) liveIframe.classList.add('hidden');
    if (dplayerContainer) dplayerContainer.classList.add('hidden');
    if (nativePlayer) nativePlayer.classList.add('hidden');
    if (emptyState) {
        resetEmptyState(index);
        emptyState.classList.remove('hidden');
        emptyState.classList.add('pointer-events-none');
    }
    if (label) {
        label.classList.add('hidden');
        label.innerText = '';
    }
    if (closeBtn) {
        closeBtn.classList.add('hidden');
        closeBtn.classList.remove('flex');
    }
    updateFocusUI();
}

function stopCellPlayback(index) {
    const liveIframe = document.getElementById(`live-iframe-${index}`);
    const nativePlayer = document.getElementById(`native-player-${index}`);

    if (dpInstances[index]) {
        dpInstances[index].destroy();
        dpInstances[index] = null;
    }
    if (nativePlayer) {
        nativePlayer.pause();
        nativePlayer.removeAttribute('src');
        nativePlayer.load();
    }
    if (liveIframe) {
        liveIframe.src = 'about:blank';
    }
}

function setActiveCell(index) {
    activeCell = index;
    updateFocusUI();
}

function updateFocusUI() {
    for (let i = 0; i < currentLayout; i++) {
        const cell = document.getElementById(`cell-${i}`);
        if (cell) {
            if (i === activeCell) {
                cell.classList.add('border-blue-500', 'shadow-[inset_0_0_20px_rgba(59,130,246,0.3)]');
                cell.classList.remove('border-gray-800');
            } else {
                cell.classList.remove('border-blue-500', 'shadow-[inset_0_0_20px_rgba(59,130,246,0.3)]');
                cell.classList.add('border-gray-800');
            }
        }
    }
    const focusTitle = cellData[activeCell] ? cellData[activeCell].title : '空闲';
    document.getElementById('currentCam').innerText = `[窗口 ${activeCell + 1}] ${focusTitle}`;
}

function selectCamera(camId) {
    currentSelectedCam = camId;
    loadStatus();
    loadRecords(camId);
}

function previewLive(camId) {
    currentSelectedCam = camId;
    loadStatus();
    playVideo(camId, true, `🟢 直播: ${camId}`);
    loadRecords(camId);
}

function playVideo(source, isLive, title) {
    cellData[activeCell] = {source, isLive, title};
    executePlayInCell(activeCell, source, isLive, title);

    if (isLive && currentLayout > 1) {
        let nextCell = (activeCell + 1) % currentLayout;
        setActiveCell(nextCell);
    } else {
        updateFocusUI();
    }
}

async function playRecord(file, title) {
    const targetCell = activeCell;
    showProbeLoadingInCell(targetCell, title);

    try {
        // 1. 合并后的完美大 MP4：所有设备直接走原生秒开播放
        if (file.name.toLowerCase().endsWith('.mp4') && file.name.includes('_merged')) {
            cellData[targetCell] = {source: file.url, isLive: false, title};
            // 传入 true，强制使用原生的 <video> 标签播放 mp4
            executePlayInCell(targetCell, file.url, false, title, true);
            updateFocusUI();
            return;
        }

        // 2. 针对零散碎片进行探测
        const resp = await fetch(`/api/record/probe?path=${encodeURIComponent(file.path)}`);
        const probe = await resp.json();

        // 探测成功，且确实是 H.265 编码
        if (probe.can_probe && probe.is_h265) {
            // 【精准识别】：iOS全系设备 + macOS纯Safari (排除 Mac 上的 Chrome/Edge)
            const isIOS = /iPod|iPhone|iPad/.test(navigator.platform) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
            const isMacSafari = /Mac/.test(navigator.platform) && /Safari/i.test(navigator.userAgent) && !/Chrome/i.test(navigator.userAgent) && !/Edg/i.test(navigator.userAgent);
            const isAppleNative = isIOS || isMacSafari;

            const supportHEVC = browserSupportsHEVC();

            if (isAppleNative || !supportHEVC) {
                // 直接拦截并抛出不支持的 UI 提示，不再走转码逻辑
                stopCellPlayback(targetCell);
                cellData[targetCell] = {source: '', isLive: false, title: `${title} · 播放受限`};

                const emptyState = document.getElementById(`empty-state-${targetCell}`);
                const label = document.getElementById(`label-${targetCell}`);
                const closeBtn = document.getElementById(`close-cell-${targetCell}`);

                if (label) {
                    label.classList.remove('hidden');
                    label.innerText = `${title} · 限制`;
                }
                if (closeBtn) {
                    closeBtn.classList.remove('hidden');
                    closeBtn.classList.add('flex');
                }
                if (emptyState) {
                    emptyState.classList.remove('hidden');
                    emptyState.classList.remove('pointer-events-none');
                    emptyState.innerHTML = `
                        <div class="max-w-[86%] rounded-lg border border-red-500/30 bg-black/70 px-4 py-3 text-center text-white shadow-xl backdrop-blur-md">
                            <div class="text-sm font-bold mb-1 text-red-400">⚠️ 播放受限</div>
                            <div class="text-xs leading-relaxed text-white/80">
                                当前设备或浏览器不支持 H.265 录像片段播放。自动合并后的录像也许不受此影响。
                            </div>
                        </div>
                    `;
                }
                updateFocusUI();
                return;
            }

            // 设备支持 H.265 时，强制走 fMP4 重封装！
            const remuxUrl = `/play_remux/${encodeURI(file.path)}`;
            cellData[targetCell] = {source: remuxUrl, isLive: false, title};

            // 传 true 强制使用原生的 <video> 标签播放 mp4 流
            const warningMsg = "流式播放：当前片段暂不支持进度条拖拽";
            executePlayInCell(targetCell, remuxUrl, false, title, true, warningMsg);
            updateFocusUI();
            return; // 直接返回，拦截默认的 TS 播放逻辑
        }
    } catch (e) {
        console.warn('编码探测失败，尝试直接播放:', e);
    }

    // 非 H.265 的 .ts 碎片，走默认播放逻辑 (HLS 或 mpegts)
    cellData[targetCell] = {source: file.url, isLive: false, title};
    // 不强制使用原生播放器
    executePlayInCell(targetCell, file.url, false, title, false);
    updateFocusUI();
}

function browserSupportsHEVC() {
    const video = document.createElement('video');
    const candidates = ['video/mp4; codecs="hvc1.1.6.L93.B0"', 'video/mp4; codecs="hev1.1.6.L93.B0"'];
    return candidates.some(type => video.canPlayType(type) !== '');
}

function isMobilePlayback() {
    return window.matchMedia('(max-width: 767px), (pointer: coarse)').matches;
}

function showProbeLoadingInCell(index, title) {
    stopCellPlayback(index);
    cellData[index] = {source: '', isLive: false, title: `检测编码: ${title}`};

    const liveIframe = document.getElementById(`live-iframe-${index}`);
    const dplayerContainer = document.getElementById(`dplayer-${index}`);
    const nativePlayer = document.getElementById(`native-player-${index}`);
    const emptyState = document.getElementById(`empty-state-${index}`);
    const label = document.getElementById(`label-${index}`);
    const closeBtn = document.getElementById(`close-cell-${index}`);

    if (liveIframe) liveIframe.classList.add('hidden');
    if (dplayerContainer) dplayerContainer.classList.add('hidden');
    if (nativePlayer) nativePlayer.classList.add('hidden');
    if (label) {
        label.classList.remove('hidden');
        label.innerText = title;
    }
    if (closeBtn) {
        closeBtn.classList.remove('hidden');
        closeBtn.classList.add('flex');
    }
    if (emptyState) {
        emptyState.classList.remove('hidden');
        emptyState.classList.add('pointer-events-none');
        emptyState.innerHTML = `
            <div class="rounded-lg border border-white/10 bg-black/45 px-4 py-2 text-xs font-bold text-white/80 backdrop-blur-md">
                正在检测编码...
            </div>
        `;
    }
    updateFocusUI();
}

function showCodecNoticeInCell(index, data) {
    stopCellPlayback(index);

    const liveIframe = document.getElementById(`live-iframe-${index}`);
    const dplayerContainer = document.getElementById(`dplayer-${index}`);
    const nativePlayer = document.getElementById(`native-player-${index}`);
    const emptyState = document.getElementById(`empty-state-${index}`);
    const label = document.getElementById(`label-${index}`);
    const closeBtn = document.getElementById(`close-cell-${index}`);

    if (!emptyState) return;

    if (liveIframe) liveIframe.classList.add('hidden');
    if (dplayerContainer) dplayerContainer.classList.add('hidden');
    if (nativePlayer) nativePlayer.classList.add('hidden');
    if (label) {
        label.classList.remove('hidden');
        label.innerText = `${data.title} · H.265`;
    }
    if (closeBtn) {
        closeBtn.classList.remove('hidden');
        closeBtn.classList.add('flex');
    }

    emptyState.classList.remove('hidden');
    emptyState.classList.remove('pointer-events-none');
    // 判断浏览器原生是否支持硬解 H.265
    const supportHEVC = browserSupportsHEVC();

    emptyState.innerHTML = `
        <div class="max-w-[86%] rounded-lg border border-white/10 bg-black/55 px-4 py-3 text-center text-white shadow-xl backdrop-blur-md">
            <div class="text-sm font-bold mb-1">当前录像为 H.265 原画</div>
            <div class="text-[10px] leading-relaxed text-white/75 mb-3">
                ${supportHEVC ? '✨ 您的设备支持 H.265 硬件解码，推荐使用原画播放，CPU 零损耗。' : '⚠️ 当前浏览器不支持直接解码。建议使用按需转码(消耗服务器性能)。'}
            </div>
            <div class="flex flex-col space-y-2 items-center">
                ${supportHEVC ? `
                <button onclick="event.stopPropagation(); playRemuxRecord(${index})" class="w-full rounded-md bg-green-600 px-3 py-1.5 text-xs font-bold text-white shadow hover:bg-green-500 active:scale-95 transition-all">
                    ▶ 原画播放 (硬解推荐)
                </button>
                ` : ''}
                ${data.canTranscode ? `
                <button onclick="event.stopPropagation(); playTranscodedRecord(${index})" class="${supportHEVC ? 'bg-gray-600 hover:bg-gray-500' : 'bg-blue-600 hover:bg-blue-500'} w-full rounded-md px-3 py-1.5 text-xs font-bold text-white shadow active:scale-95 transition-all">
                    ${supportHEVC ? '备用：服务器转码播放' : '▶ 兼容模式 (服务器转码)'}
                </button>
                ` : ''}
            </div>
        </div>
    `;
}

// 增加对应的播放函数
function playRemuxRecord(index) {
    const data = cellData[index];
    if (!data || !data.file) return;

    activeCell = index;
    // 指向新的 fMP4 重封装后端接口
    const remuxUrl = `/play_remux/${encodeURI(data.file.path)}`;
    const title = `硬解播放: ${data.title}`;
    cellData[index] = {source: remuxUrl, isLive: false, title};

    // 这种模式下，直接交给原生 <video> 标签播放
    executePlayInCell(index, remuxUrl, false, title, true);
    updateFocusUI();
}

function playTranscodedRecord(index) {
    const data = cellData[index];
    if (!data || !data.file) return;

    activeCell = index;
    const transcodeUrl = `/play_transcode/${encodeURI(data.file.path)}`;
    const title = `转码播放: ${data.title}`;
    cellData[index] = {source: transcodeUrl, isLive: false, title};
    executePlayInCell(index, transcodeUrl, false, title);
    updateFocusUI();
}

function executePlayInCell(index, source, isLive, title, forceNative = false, warningMsg = null) {
    const liveIframe = document.getElementById(`live-iframe-${index}`);
    const dplayerContainer = document.getElementById(`dplayer-${index}`);
    const nativePlayer = document.getElementById(`native-player-${index}`);
    const emptyState = document.getElementById(`empty-state-${index}`);
    const label = document.getElementById(`label-${index}`);
    const closeBtn = document.getElementById(`close-cell-${index}`);

    if (!liveIframe) return;

    // 警告提示挂载逻辑
    let existingWarning = document.getElementById(`cell-warning-${index}`);
    if (existingWarning) existingWarning.remove(); // 清除之前的残留警告

    if (warningMsg) {
        const cell = document.getElementById(`cell-${index}`);
        const warningEl = document.createElement('div');
        warningEl.id = `cell-warning-${index}`;
        // 居中显示在顶部，加入 Tailwind 动画和毛玻璃效果，鼠标穿透(pointer-events-none)不阻挡点击
        warningEl.className = 'absolute top-5 left-1/2 -translate-x-1/2 z-30 bg-amber-500/90 text-white px-3 py-1.5 text-xs rounded-full shadow-[0_0_15px_rgba(245,158,11,0.4)] font-bold flex items-center pointer-events-none transition-all duration-1000';
        warningEl.innerHTML = `
            <svg class="w-4 h-4 mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path>
            </svg>
            ${warningMsg}
        `;
        cell.appendChild(warningEl);

        // 8 秒后自动触发淡出动画并移除，避免永久遮挡画面
        setTimeout(() => {
            const el = document.getElementById(`cell-warning-${index}`);
            if (el) {
                el.classList.add('opacity-0', '-translate-y-2');
                setTimeout(() => el.remove(), 1000);
            }
        }, 8000);
    }

    stopCellPlayback(index);
    resetEmptyState(index);
    emptyState.classList.add('hidden');
    emptyState.classList.add('pointer-events-none');
    label.classList.remove('hidden');
    label.innerText = title;
    if (closeBtn) {
        closeBtn.classList.remove('hidden');
        closeBtn.classList.add('flex');
    }

    if (isLive) {
        dplayerContainer.classList.add('hidden');
        nativePlayer.classList.add('hidden');
        liveIframe.classList.remove('hidden');
        liveIframe.src = `/stream.html?src=${source}`;
    } else {
        liveIframe.classList.add('hidden');
        liveIframe.src = '';

        // 【精准识别】：iOS全系设备 + macOS纯Safari
        const isIOS = /iPod|iPhone|iPad/.test(navigator.platform) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
        const isMacSafari = /Mac/.test(navigator.platform) && /Safari/i.test(navigator.userAgent) && !/Chrome/i.test(navigator.userAgent) && !/Edg/i.test(navigator.userAgent);
        const isAppleNative = isIOS || isMacSafari;

        const isTranscodedStream = source.startsWith('/play_transcode/');
        const isRemuxStream = source.startsWith('/play_remux/');

        // 如果强制原生，或是转码/重封装流，或在苹果上，都用原生播放器
        if (forceNative || isRemuxStream || (isAppleNative && !isTranscodedStream)) {
            dplayerContainer.classList.add('hidden');
            nativePlayer.classList.remove('hidden');
            let playUrl = source;
            // 只有原始的 TS 文件才去走 HLS，转码和重封装的流绝对不能改前缀
            if (source.endsWith('.ts') && !isRemuxStream && !isTranscodedStream) {
                playUrl = source.replace('/play/', '/play_hls/');
            }
            nativePlayer.src = playUrl;
            nativePlayer.play().catch(e => console.log("等待交互播放"));
        } else {
            nativePlayer.classList.add('hidden');
            dplayerContainer.classList.remove('hidden');
            let videoType = source.endsWith('.ts') ? 'customTs' : 'normal';

            dpInstances[index] = new DPlayer({
                container: dplayerContainer, video: {
                    url: source, type: videoType, customType: {
                        customTs: function (video, player) {
                            const tsPlayer = mpegts.createPlayer({type: 'm2ts', isLive: false, url: video.src});
                            tsPlayer.attachMediaElement(video);
                            tsPlayer.load();
                            tsPlayer.play();
                            player.events.on('destroy', () => {
                                tsPlayer.destroy();
                            });
                        }
                    }
                }
            });
        }
    }
}

function resetEmptyState(index) {
    const emptyState = document.getElementById(`empty-state-${index}`);
    if (!emptyState) return;

    emptyState.innerHTML = `
        <svg class="w-8 h-8 mb-2 opacity-30" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
        <span class="text-xs font-bold tracking-wider uppercase opacity-50">窗口 ${index + 1}</span>
    `;
}

// --- 历史录像逻辑 ---
async function loadRecords(camId) {
    const list = document.getElementById('recordList');
    const countBadge = document.getElementById('recordCount');
    list.innerHTML = '<div class="col-span-full text-center py-10 text-gray-400">检索中...</div>';

    try {
        const resp = await fetch(`/api/records/${camId}`);
        const files = await resp.json();
        list.innerHTML = '';

        if (!files || files.length === 0) {
            countBadge.innerText = '0 个文件';
            list.innerHTML = '<div class="col-span-full text-center py-12 text-gray-400">该设备暂无历史录像</div>';
            return;
        }

        countBadge.innerText = `${files.length} 个文件`;
        const groups = {};
        files.forEach(file => {
            const match = file.name.match(/\d{4}-\d{2}-\d{2}/);
            const dateStr = match ? match[0] : '其他归档';
            if (!groups[dateStr]) groups[dateStr] = [];
            groups[dateStr].push(file);
        });

        const sortedDates = Object.keys(groups).sort((a, b) => b.localeCompare(a));
        sortedDates.forEach((date, index) => {
            const dateFiles = groups[date];
            dateFiles.sort((a, b) => b.name.localeCompare(a.name));
            const groupId = `date-${index}`;
            const isFirst = index === 0;
            const groupDiv = document.createElement('div');
            groupDiv.className = 'flex flex-col bg-gray-50 border border-gray-200 rounded-xl overflow-hidden shadow-sm';

            const header = document.createElement('div');
            header.className = 'flex justify-between items-center p-3.5 bg-white border-b border-gray-200 cursor-pointer hover:bg-blue-50 transition-colors select-none';
            header.onclick = () => {
                document.getElementById(`content-${groupId}`).classList.toggle('hidden');
                document.getElementById(`icon-${groupId}`).classList.toggle('rotate-90');
            };
            header.innerHTML = `
                <div class="flex items-center">
                    <span class="text-sm font-bold text-gray-700">${date}</span>
                    <span class="ml-2 text-[10px] font-bold bg-blue-100 text-blue-600 px-2 py-0.5 rounded-full">${dateFiles.length}</span>
                </div>
                <svg id="icon-${groupId}" class="w-4 h-4 text-gray-400 transition-transform ${isFirst ? 'rotate-90' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path></svg>
            `;

            const content = document.createElement('div');
            content.id = `content-${groupId}`;
            content.className = `flex flex-col space-y-1 p-2 max-h-[300px] overflow-y-auto custom-scrollbar ${isFirst ? '' : 'hidden'}`;

            dateFiles.forEach(file => {
                const timeMatch = file.name.match(/\d{2}-\d{2}-\d{2}\.(ts|mp4)$/);
                let timeDisplay = timeMatch ? timeMatch[0].replace(/\.(ts|mp4)$/, '').replace(/-/g, ':') : file.name;
                const ext = file.name.split('.').pop().toUpperCase();
                const item = document.createElement('div');
                item.className = 'flex justify-between items-center p-2.5 text-xs rounded-lg bg-white border border-gray-100 hover:border-blue-400 hover:text-blue-600 cursor-pointer transition-all shadow-sm group';
                item.onclick = () => {
                    playRecord(file, `🎬 回放: ${camId} (${timeDisplay})`);
                };
                item.innerHTML = `
                <div class="flex items-center">
                    <span class="mr-2">🎬</span>
                    <span class="font-mono font-bold">${timeDisplay}</span>
                </div>
                <div class="flex items-center space-x-2">
                    <span class="text-[10px] text-gray-400 font-mono">${file.size}</span>
                    <span class="text-[9px] px-1 bg-gray-50 text-gray-400 rounded border border-gray-100 font-bold">${ext}</span>
                    <button onclick="deleteRecord(event, '${camId}', '${file.path}')" class="text-red-400 hover:text-red-600 opacity-0 group-hover:opacity-100 transition-opacity px-1" title="永久删除该录像">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
                    </button>
                </div>
            `;
                content.appendChild(item);
            });
            groupDiv.appendChild(header);
            groupDiv.appendChild(content);
            list.appendChild(groupDiv);
        });
    } catch (e) {
        list.innerHTML = '<div class="col-span-full text-center py-10 text-red-400">获取录像列表失败</div>';
    }
}

async function deleteRecord(e, camId, filePath) {
    e.stopPropagation();
    if (!confirm('确定要永久删除此录像文件吗？释放的空间不可恢复。')) return;

    try {
        const resp = await fetch(`/api/record?path=${encodeURIComponent(filePath)}`, {method: 'DELETE'});
        if (resp.ok) {
            loadRecords(camId);
        } else {
            const err = await resp.json();
            alert('操作失败: ' + err.error);
        }
    } catch (err) {
        alert('网络请求失败，请检查连接状态');
    }
}

// 1. 矩阵整体全屏
function toggleMatrixFullscreen() {
    const wrapper = document.getElementById("video-wrapper");
    if (!document.fullscreenElement && !document.webkitFullscreenElement) {
        if (wrapper.requestFullscreen) {
            wrapper.requestFullscreen();
        } else if (wrapper.webkitRequestFullscreen) {
            wrapper.webkitRequestFullscreen();
        }
    } else {
        if (document.exitFullscreen) {
            document.exitFullscreen();
        } else if (document.webkitExitFullscreen) {
            document.webkitExitFullscreen();
        }
    }
}

// 2. 单个格子双击独立全屏
function toggleCellFullscreen(index) {
    const cell = document.getElementById(`cell-${index}`);
    // 阻止事件冒泡防止触发点击的焦点切换
    if (!document.fullscreenElement && !document.webkitFullscreenElement) {
        if (cell.requestFullscreen) cell.requestFullscreen(); else if (cell.webkitRequestFullscreen) cell.webkitRequestFullscreen();
    } else {
        if (document.exitFullscreen) document.exitFullscreen(); else if (document.webkitExitFullscreen) document.webkitExitFullscreen();
    }
}

// 3. 监听全局全屏状态变化，自动去圆角、去边框、切图标，达到完美沉浸感
['fullscreenchange', 'webkitfullscreenchange'].forEach(eventType => {
    document.addEventListener(eventType, () => {
        const enterIcon = document.getElementById('icon-fullscreen-enter');
        const exitIcon = document.getElementById('icon-fullscreen-exit');
        const wrapper = document.getElementById('video-wrapper');

        // 只要是在全屏状态下 (无论是多宫格全屏，还是单格子全屏)
        if (document.fullscreenElement || document.webkitFullscreenElement) {
            enterIcon.classList.add('hidden');
            exitIcon.classList.remove('hidden');

            // 去除父容器圆角和边框，贴合显示器物理边缘
            wrapper.classList.remove('rounded-xl', 'border');
            wrapper.classList.add('rounded-none', 'border-0');
        } else {
            enterIcon.classList.remove('hidden');
            exitIcon.classList.add('hidden');

            // 退出全屏，恢复 UI 质感
            wrapper.classList.add('rounded-xl', 'border');
            wrapper.classList.remove('rounded-none', 'border-0');
        }
    });
});
