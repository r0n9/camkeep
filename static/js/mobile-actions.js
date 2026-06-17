(function () {
    let cameraPickerFilter = 'all';

    function escapeHtmlValue(value) {
        return String(value == null ? '' : value).replace(/[&<>"']/g, char => ({
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#39;'
        })[char]);
    }

    function getMobileAPI() {
        return window.CamKeepMobile || null;
    }

    function getCameraEntries() {
        if (typeof window.getMobileCameraEntries === 'function') return window.getMobileCameraEntries();
        if (!Array.isArray(window.latestCameraStatusEntries)) return [];
        return window.latestCameraStatusEntries;
    }

    function cameraOnlineState(cam) {
        const running = cam?.is_running === true || cam?.running === true;
        const streamState = String(cam?.stream_state || cam?.state || '').toLowerCase();
        const recordState = String(cam?.record_state || '').toLowerCase();
        if (running || streamState === 'online' || streamState === 'running') return 'online';
        if (recordState === 'recording') return 'online';
        if (streamState === 'offline' || cam?.online === false) return 'offline';
        return 'idle';
    }

    function cameraMetaText(cam) {
        const parts = [];
        if (cam?.mode) parts.push(cam.mode);
        if (cam?.record_state) parts.push(cam.record_state);
        const state = cameraOnlineState(cam);
        parts.push(state === 'online' ? '在线' : (state === 'offline' ? '离线' : '待连接'));
        return parts.join(' · ');
    }

    function syncCameraPickerList(wrapper, entries) {
        const list = wrapper?.querySelector('.mobile-picker-list');
        const toolbar = wrapper?.querySelector('.mobile-picker-toolbar');
        if (!list || !toolbar) return;
        toolbar.querySelectorAll('[data-filter]').forEach(button => {
            button.classList.toggle('is-active', button.dataset.filter === cameraPickerFilter);
        });
        const filtered = entries.filter(({cam}) => cameraPickerFilter === 'all' || cameraOnlineState(cam) === cameraPickerFilter);
        list.innerHTML = '';
        if (!entries.length) {
            list.innerHTML = '<div class="mobile-sheet-empty">暂无可访问的摄像头</div>';
            return;
        }
        if (!filtered.length) {
            list.innerHTML = '<div class="mobile-sheet-empty">当前筛选条件下没有摄像头</div>';
            return;
        }

        filtered.forEach(({id, cam}) => {
            const state = cameraOnlineState(cam);
            const item = document.createElement('div');
            const selectedCam = typeof window.getCurrentSelectedCam === 'function' ? window.getCurrentSelectedCam() : window.currentSelectedCam;
            item.className = `mobile-picker-item ${selectedCam === id ? 'is-selected' : ''}`;
            item.innerHTML = `
                <span class="mobile-picker-dot is-${state === 'online' ? 'online' : state === 'offline' ? 'offline' : 'idle'}" aria-hidden="true">
                    <svg width="17" height="17" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 10l4.5-2.25A1 1 0 0121 8.64v6.72a1 1 0 01-1.5.87L15 14M5 6h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2z"></path>
                    </svg>
                </span>
                <span class="mobile-picker-main">
                    <strong class="mobile-picker-title">${escapeHtmlValue(id)}</strong>
                    <span class="mobile-picker-meta">${escapeHtmlValue(cameraMetaText(cam))}</span>
                </span>
                <span class="mobile-picker-actions">
                    <button type="button" class="mobile-picker-action mobile-picker-action--primary" data-preview-camera="${escapeHtmlValue(id)}">直播</button>
                    <button type="button" class="mobile-picker-action" data-select-camera="${escapeHtmlValue(id)}">录像</button>
                </span>
            `;
            item.querySelector('[data-select-camera]')?.addEventListener('click', event => {
                event.stopPropagation();
                if (typeof window.openCameraRecordsFromNode === 'function') {
                    window.openCameraRecordsFromNode(id);
                } else {
                    window.selectCamera?.(id);
                    getMobileAPI()?.setTab('records', {instant: true});
                }
                getMobileAPI()?.closeSheet?.();
            });
            item.querySelector('[data-preview-camera]')?.addEventListener('click', event => {
                event.stopPropagation();
                if (typeof window.openCameraLiveFromNode === 'function') {
                    window.openCameraLiveFromNode(id);
                } else {
                    window.previewLive?.(id);
                    getMobileAPI()?.setTab('monitor', {instant: true});
                }
                getMobileAPI()?.closeSheet?.();
            });
            item.addEventListener('click', () => {
                if (typeof window.openCameraLiveFromNode === 'function') {
                    window.openCameraLiveFromNode(id);
                } else {
                    window.previewLive?.(id);
                    getMobileAPI()?.setTab('monitor', {instant: true});
                }
                getMobileAPI()?.closeSheet?.();
            });
            list.appendChild(item);
        });
    }

    function openCameraPicker() {
        const entries = getCameraEntries();
        const wrapper = document.createElement('div');
        wrapper.className = 'mobile-picker-wrap';
        const toolbar = document.createElement('div');
        toolbar.className = 'mobile-picker-toolbar';
        toolbar.innerHTML = `
            <button type="button" class="mobile-picker-filter is-active" data-filter="all">全部</button>
            <button type="button" class="mobile-picker-filter" data-filter="online">在线</button>
            <button type="button" class="mobile-picker-filter" data-filter="idle">待连接</button>
            <button type="button" class="mobile-picker-filter" data-filter="offline">离线</button>
        `;
        toolbar.querySelectorAll('[data-filter]').forEach(button => {
            button.addEventListener('click', () => {
                cameraPickerFilter = button.dataset.filter || 'all';
                syncCameraPickerList(wrapper, entries);
            });
        });
        wrapper.appendChild(toolbar);
        const list = document.createElement('div');
        list.className = 'mobile-picker-list';
        wrapper.appendChild(list);
        syncCameraPickerList(wrapper, entries);
        return getMobileAPI()?.openSheet('选择设备', `${entries.length} 个实时节点`, wrapper) || false;
    }

    function formatDateKey(date) {
        const year = date.getFullYear();
        const month = String(date.getMonth() + 1).padStart(2, '0');
        const day = String(date.getDate()).padStart(2, '0');
        return `${year}-${month}-${day}`;
    }

    function applyRecordQuickRange(kind) {
        const startInput = document.getElementById('recordStartDate');
        const endInput = document.getElementById('recordEndDate');
        if (!startInput || !endInput) return;
        const today = new Date();
        const start = new Date(today);
        if (kind === 'yesterday') {
            start.setDate(today.getDate() - 1);
            today.setDate(today.getDate() - 1);
        } else if (kind === '3d') {
            start.setDate(today.getDate() - 2);
        } else if (kind === '7d') {
            start.setDate(today.getDate() - 6);
        }
        startInput.value = formatDateKey(start);
        endInput.value = formatDateKey(today);
        document.querySelectorAll('.mobile-record-range-quick button').forEach(button => {
            button.classList.toggle('is-active', button.getAttribute('onclick')?.includes(`'${kind}'`) === true);
        });
        window.applyRecordRange?.();
    }

    function openRecordActionSheet(recordPath, camId = (typeof window.getCurrentSelectedCam === 'function' ? window.getCurrentSelectedCam() : window.currentSelectedCam) || '') {
        recordPath = String(recordPath || '').trim();
        if (!recordPath) return false;
        const fileName = recordPath.split('/').pop() || 'record';
        const canDelete = typeof window.canAdmin === 'function' ? window.canAdmin() : false;
        const wrapper = document.createElement('div');
        wrapper.className = 'mobile-sheet-actions';
        wrapper.innerHTML = `
            <button type="button" class="mobile-sheet-action" data-action="play">
                <span class="mobile-sheet-action-icon"><svg width="17" height="17" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 5v14l11-7-11-7z"></path></svg></span>
                <span class="mobile-sheet-action-copy"><strong>播放录像</strong><span>${escapeHtmlValue(fileName)}</span></span>
            </button>
            <button type="button" class="mobile-sheet-action" data-action="download">
                <span class="mobile-sheet-action-icon"><svg width="17" height="17" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1M8 12l4 4m0 0l4-4m-4 4V4"></path></svg></span>
                <span class="mobile-sheet-action-copy"><strong>下载录像</strong><span>保存原始录像文件</span></span>
            </button>
            ${canDelete ? `<button type="button" class="mobile-sheet-action mobile-sheet-action--danger" data-action="delete">
                <span class="mobile-sheet-action-icon"><svg width="17" height="17" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg></span>
                <span class="mobile-sheet-action-copy"><strong>删除录像</strong><span>永久删除，释放存储空间</span></span>
            </button>` : ''}
        `;
        wrapper.querySelector('[data-action="play"]')?.addEventListener('click', () => {
            getMobileAPI()?.closeSheet?.();
            window.playRecordByPath?.(recordPath);
        });
        wrapper.querySelector('[data-action="download"]')?.addEventListener('click', () => {
            getMobileAPI()?.closeSheet?.();
            window.downloadRecord?.({stopPropagation() {}}, recordPath);
        });
        wrapper.querySelector('[data-action="delete"]')?.addEventListener('click', () => {
            getMobileAPI()?.closeSheet?.();
            window.deleteRecord?.({stopPropagation() {}}, camId, recordPath);
        });
        return getMobileAPI()?.openSheet('录像操作', fileName, wrapper) || false;
    }

    function syncMobileActionState() {
        document.querySelectorAll('.mobile-record-range-quick button').forEach(button => {
            button.classList.remove('is-active');
        });
    }

    window.CamKeepMobileActions = {
        applyRecordQuickRange,
        openCameraPicker,
        openRecordActionSheet,
        syncMobileActionState
    };

    window.openCameraPicker = openCameraPicker;
    window.applyRecordQuickRange = applyRecordQuickRange;
    window.openRecordActionSheet = openRecordActionSheet;
})();
