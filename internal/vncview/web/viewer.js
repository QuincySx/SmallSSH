import RFB from './novnc/core/rfb.js';
import KeyTable from './novnc/core/input/keysym.js';
import {setupClipboard} from './clipboard.js';

const $ = id => document.getElementById(id);
let rfb;
let required = [];
let viaSSH = false;
let savedCredentials = {};
let pendingCredentials;
let noticeTimer;
function notify(message) {
    clearTimeout(noticeTimer);
    $('notice').textContent = message; $('notice').hidden = false;
    noticeTimer = setTimeout(() => { $('notice').hidden = true; $('notice').textContent = ''; }, 3500);
}
const clipboard = setupClipboard(() => rfb, notify);
const modifiers = [
    ['mod-ctrl', KeyTable.XK_Control_L, 'ControlLeft'],
    ['mod-alt', KeyTable.XK_Alt_L, 'AltLeft'],
    ['mod-shift', KeyTable.XK_Shift_L, 'ShiftLeft'],
    ['mod-super', KeyTable.XK_Super_L, 'MetaLeft'],
];
const remoteKeys = new Map([
    ['Escape', ['Esc', KeyTable.XK_Escape]], ['Tab', ['Tab', KeyTable.XK_Tab]],
    ['Enter', ['Enter', KeyTable.XK_Return]], ['Backspace', ['Backspace', KeyTable.XK_BackSpace]],
    ['Delete', ['Delete', KeyTable.XK_Delete]], ['Insert', ['Insert', KeyTable.XK_Insert]],
    ['Home', ['Home', KeyTable.XK_Home]], ['End', ['End', KeyTable.XK_End]],
    ['PageUp', ['Page Up', KeyTable.XK_Page_Up]], ['PageDown', ['Page Down', KeyTable.XK_Page_Down]],
    ['ArrowUp', ['↑', KeyTable.XK_Up]], ['ArrowDown', ['↓', KeyTable.XK_Down]],
    ['ArrowLeft', ['←', KeyTable.XK_Left]], ['ArrowRight', ['→', KeyTable.XK_Right]],
    ['Space', ['Space', 0x20]], ['Backquote', ['`', 0x60, 0x7e]],
    ['MetaLeft', ['Win / Super', KeyTable.XK_Super_L]],
]);
for (let n = 1; n <= 12; n++) remoteKeys.set('F' + n, ['F' + n, KeyTable['XK_F' + n]]);
for (const letter of 'ABCDEFGHIJKLMNOPQRSTUVWXYZ') remoteKeys.set('Key' + letter, [letter, letter.toLowerCase().charCodeAt(0), letter.charCodeAt(0)]);
for (const digit of '0123456789') remoteKeys.set('Digit' + digit, [digit, digit.charCodeAt(0), ')!@#$%^&*('[Number(digit)].charCodeAt(0)]);
for (const [code, [label]] of remoteKeys) {
    if (['Escape', 'Tab', 'MetaLeft'].includes(code)) continue;
    const button = document.createElement('button');
    button.dataset.key = code;
    button.textContent = label;
    $('key-grid').append(button);
}

function updateModifiers() {
    const held = modifiers.filter(([id]) => $(id).getAttribute('aria-pressed') === 'true');
    $('keyboard-toggle').dataset.held = String(held.length > 0);
    $('keyboard-toggle').title = held.length ? '远程按键 · ' + held.map(([id]) => $(id).textContent).join('+') + ' 已按住' : '远程按键';
}
function releaseModifiers() {
    for (const [id, sym, code] of [...modifiers].reverse()) {
        if ($(id).getAttribute('aria-pressed') !== 'true') continue;
        rfb?.sendKey(sym, code, false);
        $(id).setAttribute('aria-pressed', 'false');
    }
    updateModifiers();
}

function closeKeyboard() {
    $('keyboard-panel').hidden = true;
    $('keyboard-toggle').setAttribute('aria-expanded', 'false');
}
function updateKeyboard() {
    const enabled = document.body.dataset.state === 'connected' && !$('view-only').checked;
    $('keyboard-toggle').disabled = !enabled;
    for (const control of $('keyboard-panel').querySelectorAll('button')) control.disabled = !enabled;
    if (!enabled) {
        closeKeyboard();
        releaseModifiers();
    }
}
function sendCombination(code) {
    if (!rfb || rfb.viewOnly || document.body.dataset.state !== 'connected') return;
    const key = remoteKeys.get(code);
    if (!key) return;
    const shifted = $('mod-shift').getAttribute('aria-pressed') === 'true';
    rfb.sendKey(shifted && key[2] ? key[2] : key[1], code);
    rfb.focus({preventScroll: true});
}
const status = (state, message) => {
    if (state !== 'connected') releaseModifiers();
    document.body.dataset.state = state;
    $('status').textContent = message;
    $('connection-indicator').title = message;
    $('connection-indicator').setAttribute('aria-label', message);
    if (state === 'failed') notify(message);
    updateKeyboard();
    clipboard.update();
    $('connect').disabled = state === 'connecting' || state === 'authenticating';
    $('disconnect').disabled = !['connected', 'connecting', 'authenticating'].includes(state);
};
const hideLogin = () => {
    $('credentials').hidden = true;
    for (const key of ['username', 'password', 'target']) $(key).value = '';
};
function connect() {
    releaseModifiers();
    if (rfb) rfb.disconnect();
    hideLogin();
    $('identity').hidden = true;
    status('connecting', '正在连接…');
    pendingCredentials = undefined;
    let usedSaved = false;
    const url = new URL('socket', location.href);
    url.protocol = 'ws:';
    const session = new RFB($('screen'), url.href, {shared: true});
    rfb = session;
    clipboard.bind(session);
    session.viewOnly = $('view-only').checked;
    session.scaleViewport = $('scale').checked;
    session.resizeSession = false;
    session.addEventListener('connect', () => {
        if (rfb !== session) return;
        status('connected', viaSSH ? '已连接 · 经 SSH' : '已连接 · 直连 VNC');
        if (pendingCredentials) {
            const value = pendingCredentials;
            pendingCredentials = undefined;
            fetch('credentials', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(value), cache:'no-store'})
                .then(response => {
                    if (!response.ok) throw new Error('save failed');
                    if (rfb === session) {
                        savedCredentials = value.remember ? value : {};
                        if (value.remember && document.body.dataset.state === 'connected') {
                            $('status').textContent += ' · 密码已保存';
                            notify('VNC 密码已保存');
                        }
                    }
                }).catch(() => {
                    if (rfb === session && document.body.dataset.state === 'connected') {
                        $('status').textContent = '已连接 · VNC 密码保存失败，下次需重新输入';
                        notify('VNC 密码保存失败，下次需重新输入');
                    }
                });
        }
    });
    session.addEventListener('disconnect', event => {
        if (rfb !== session) return;
        hideLogin();
        $('identity').hidden = true;
        if (document.body.dataset.state !== 'failed') status(event.detail.clean ? 'disconnected' : 'failed', event.detail.clean ? '已断开' : '连接已断开，请检查 VNC 服务或 SSH 连接');
    });
    session.addEventListener('credentialsrequired', event => {
        if (rfb !== session) return;
        required = event.detail.types;
        if (required.some(key => !['username', 'password', 'target'].includes(key))) {
            status('failed', '此 VNC 服务请求了不支持的登录字段');
            session.disconnect();
            return;
        }
        status('authenticating', '等待 VNC 登录');
        if (!usedSaved && savedCredentials.password && required.every(key => savedCredentials[key])) {
            usedSaved = true;
            session.sendCredentials(Object.fromEntries(required.map(key => [key, savedCredentials[key]])));
            return;
        }
        for (const key of ['username', 'password', 'target']) $(key + '-row').hidden = !required.includes(key);
        $('username').value = savedCredentials.username || '';
        $('credentials').hidden = false;
        $(required[0] || 'password').focus();
    });
    session.addEventListener('securityfailure', event => {
        if (rfb === session) {
            savedCredentials = {}; pendingCredentials = undefined;
            status('failed', 'VNC 登录失败，请重新连接后输入密码' + (event.detail.reason ? '：' + String(event.detail.reason).slice(0, 300) : ''));
        }
    });
    session.addEventListener('serververification', async event => {
        if (rfb !== session) return;
        if (event.detail.type !== 'RSA') {
            status('failed', '无法核对该 VNC 服务器的身份类型');
            session.disconnect();
            return;
        }
        try {
            const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', event.detail.publickey));
            if (rfb !== session) return;
            $('fingerprint').textContent = 'SHA256:' + btoa(String.fromCharCode(...digest)).replace(/=+$/, '');
            $('identity').hidden = false;
        } catch {
            status('failed', '无法计算 VNC 服务器指纹，连接已停止');
            session.disconnect();
        }
    });
}
$('login').addEventListener('submit', event => {
    event.preventDefault();
    const credentials = {};
    for (const key of required) credentials[key] = $(key).value;
    pendingCredentials = {username:credentials.username || '', password:credentials.password || '', remember:$('remember-password').checked};
    rfb.sendCredentials(credentials);
    hideLogin();
    status('connecting', '正在验证 VNC 登录…');
});
$('cancel-login').onclick = () => { hideLogin(); rfb?.disconnect(); };
$('cancel-identity').onclick = () => { $('identity').hidden = true; rfb?.disconnect(); };
$('trust').onclick = () => { $('identity').hidden = true; rfb?.approveServer(); };
$('connect').onclick = connect;
$('disconnect').onclick = () => { releaseModifiers(); rfb?.disconnect(); };
function closeMore() {
    $('more-panel').hidden = true;
    $('more-toggle').setAttribute('aria-expanded', 'false');
}
$('more-toggle').onclick = () => {
    const open = $('more-panel').hidden;
    closeKeyboard();
    $('more-panel').hidden = !open;
    $('more-toggle').setAttribute('aria-expanded', String(open));
};
$('more-panel').addEventListener('click', event => { if (event.target.closest('button')) closeMore(); });
$('keyboard-toggle').onclick = () => {
    if ($('keyboard-toggle').disabled) return;
    closeMore();
    $('keyboard-panel').hidden = !$('keyboard-panel').hidden;
    $('keyboard-toggle').setAttribute('aria-expanded', String(!$('keyboard-panel').hidden));
};
document.addEventListener('pointerdown', event => {
    if (!$('keyboard-panel').contains(event.target) && !$('keyboard-toggle').contains(event.target)) closeKeyboard();
    if (!$('more-panel').contains(event.target) && !$('more-toggle').contains(event.target)) closeMore();
});
document.addEventListener('keydown', event => {
    if (event.key !== 'Escape') return;
    const toggle = !$('keyboard-panel').hidden ? $('keyboard-toggle') : !$('more-panel').hidden ? $('more-toggle') : null;
    if (!toggle) return;
    event.preventDefault(); event.stopPropagation();
    closeKeyboard(); closeMore();
    (toggle.getClientRects().length ? toggle : $('more-toggle')).focus();
}, true);
$('fullscreen').onclick = async () => {
    $('fullscreen').disabled = true;
    try {
        const response = await fetch('fullscreen', {method:'POST', headers:{'Content-Type':'application/json'}, body:'{}', cache:'no-store'});
        if (!response.ok) throw new Error('fullscreen unavailable');
    } catch { notify('无法切换全屏，请重试'); }
    finally { $('fullscreen').disabled = false; }
};
$('close-keyboard').onclick = () => {
    closeKeyboard();
    ($('keyboard-toggle').getClientRects().length ? $('keyboard-toggle') : $('more-toggle')).focus();
};
for (const button of document.querySelectorAll('[data-key]')) button.onclick = () => sendCombination(button.dataset.key);
for (const [id, sym, code] of modifiers) $(id).onclick = () => {
    if (!rfb || rfb.viewOnly || document.body.dataset.state !== 'connected') return;
    const down = $(id).getAttribute('aria-pressed') !== 'true';
    rfb.sendKey(sym, code, down);
    $(id).setAttribute('aria-pressed', String(down));
    updateModifiers();
    rfb.focus({preventScroll: true});
};
$('keys').onclick = () => { if (rfb && !rfb.viewOnly) { releaseModifiers(); rfb.sendCtrlAltDel(); rfb.focus({preventScroll: true}); } };
$('view-only').onchange = () => { releaseModifiers(); if (rfb) rfb.viewOnly = $('view-only').checked; updateKeyboard(); clipboard.update(); };
$('scale').onchange = () => { if (rfb) rfb.scaleViewport = $('scale').checked; };
window.addEventListener('blur', releaseModifiers);
document.addEventListener('visibilitychange', () => { if (document.hidden) releaseModifiers(); });
window.addEventListener('pagehide', () => { releaseModifiers(); savedCredentials = {}; pendingCredentials = undefined; hideLogin(); clipboard.clear(); clearTimeout(noticeTimer); rfb?.disconnect(); });

// Move the existing controls, preserving their state and listeners. Overflow
// never duplicates clipboard actions or reconnects the underlying RFB session.
const primaryTools = [...$('toolbar-primary').children];
function layoutToolbar() {
    const rail = $('toolbar');
    const available = rail.clientHeight - 8 - 28;
    const fits = available >= primaryTools.length * 36 - 4;
    // Reserve the overflow button only when the complete toolbar cannot fit.
    const count = fits ? primaryTools.length : Math.max(0, Math.floor((available - 32) / 36));
    $('more-toggle').hidden = fits;
    if (fits) closeMore();
    const primary = primaryTools.slice(0, count), overflow = primaryTools.slice(count);
    if (primary.every((tool, i) => $('toolbar-primary').children[i] === tool) && $('toolbar-primary').children.length === primary.length) return;
    $('toolbar-primary').replaceChildren(...primary);
    $('overflow-actions').replaceChildren(...overflow);
}
new ResizeObserver(layoutToolbar).observe($('toolbar'));
layoutToolbar();
try {
    const response = await fetch('config.json', {cache: 'no-store'});
    if (!response.ok) throw new Error('configuration unavailable');
    const config = await response.json();
    $('view-only').checked = !!config.viewOnly;
    viaSSH = !!config.viaSSH;
    savedCredentials = config.credentials || {};
    $('remember-password').checked = config.credentials?.remember !== false;
    connect();
} catch {
    status('failed', '无法打开远程桌面，请关闭窗口后重新连接');
}
