// Clipboard data stays in this viewer. Explicit buttons use the native host;
// browser clipboard APIs would display an extra WebView paste confirmation.
const limit = 64 * 1024;
const encoder = new TextEncoder();

function validate(text) {
    if (text.includes('\0')) return '仅支持不含 NUL 的纯文本';
    if (encoder.encode(text).length > limit) return '文字超过 64 KiB，请分段发送';
    return '';
}

// noVNC 1.7.0 has no public Unicode clipboard capability. Keep this one
// read-only compatibility probe aligned with the pinned clipboardPasteFrom
// branch, so legacy Latin-1 servers cannot silently turn Chinese into "?".
function supportsUnicode(session) {
    return !!(session?._clipboardServerCapabilitiesFormats?.[1] &&
        session?._clipboardServerCapabilitiesActions?.[1 << 27]);
}

async function nativeClipboard(action, text = '') {
    const response = await fetch('clipboard', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({action, text}), cache:'no-store'});
    if (response.status === 422) throw new Error('剪贴板文字超过 64 KiB 或含无效字符');
    if (!response.ok) throw new Error(action === 'read' ? '无法读取本机剪贴板，请重试' : '复制失败，请重试');
    return (await response.json()).text;
}

export function setupClipboard(getSession, message) {
    const $ = id => document.getElementById(id);
    let generation = 0, revision = 0;
    let text = '', received = false, enabled = false;
    const current = (session, sequence) => session === getSession() && sequence === generation && enabled;
    const clear = () => {
        generation++; revision++;
        text = ''; received = false;
        $('copy-clipboard').disabled = true;
    };
    function update() {
        const active = document.body.dataset.state === 'connected' && !$('view-only').checked;
        if (!active && enabled) clear();
        enabled = active;
        $('paste-clipboard').disabled = !enabled;
        $('copy-clipboard').disabled = !enabled || !received;
    }
    function bind(session) {
        clear();
        session.addEventListener('clipboard', event => {
            if (getSession() !== session || !enabled) return;
            revision++;
            const error = validate(event.detail.text);
            text = error ? '' : event.detail.text;
            received = !error;
            $('copy-clipboard').disabled = !received;
            if (error) message('未接收远端文字：' + error);
        });
    }
    $('paste-clipboard').onclick = async () => {
        if (!enabled) return;
        const session = getSession(), sequence = generation, version = revision;
        try {
            const value = await nativeClipboard('read');
            if (!current(session, sequence) || revision !== version) return;
            const error = validate(value);
            if (error) { message(error); return; }
            if (!supportsUnicode(session) && /[^\u0000-\u00ff]/u.test(value)) {
                message('此 VNC 服务不支持 Unicode 剪贴板，无法发送中文等字符');
                return;
            }
            session.clipboardPasteFrom(value);
            revision++;
            text = value; received = true;
            $('copy-clipboard').disabled = false;
            message('已粘贴到远端剪贴板');
        } catch (error) {
            if (current(session, sequence) && revision === version) message(error.message || '粘贴失败，请重试');
        }
    };
    $('copy-clipboard').onclick = async () => {
        if (!enabled || !received) return;
        const session = getSession(), sequence = generation, version = revision, value = text;
        try {
            await nativeClipboard('write', value);
            if (current(session, sequence) && revision === version) message('复制成功');
        } catch {
            if (current(session, sequence) && revision === version) message('复制失败，请重试');
        }
    };
    return {bind, update, clear};
}
