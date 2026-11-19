// The terminal page: a shell on this server over a WebSocket, drawn by
// ghostty-web (Ghostty's terminal emulator compiled to WebAssembly).
// Looks like the control center's terminal and the default Ghostty setup:
// JetBrains Mono 16, Carbonfox colors, bar cursor.
// ⌘F finds, ⌘+ / ⌘- / ⌘0 change the font size (Ctrl+Shift on other systems),
// ⌘-click opens a link, Enter reconnects after the connection was lost.

import {FitAddon, Terminal, init} from '/public/vendor/ghostty-web-0.4.0/ghostty-web.js';
import {createSearch} from '/public/js/terminal-search.js';

const FONT_FAMILY = '"JetBrainsMono Nerd Font", "JetBrains Mono", "SF Mono", Menlo, Consolas, "DejaVu Sans Mono", monospace';
const DEFAULT_FONT_SIZE = 16;
const PREFS_KEY = 'uranus.terminal';
// Ghostty limits the scrollback in bytes; this is its default (about 10,000 lines).
const SCROLLBACK_BYTES = 10_000_000;

// Carbonfox as in the control center. Like Ghostty with selection-invert-fg-bg,
// a selection (and the current search match) shows the colors swapped.
const THEME = {
    background: '#1d1d1d', foreground: '#f6f825', cursor: '#f2f4f8', cursorAccent: '#1d1d1d',
    selectionBackground: '#f6f825', selectionForeground: '#1d1d1d',
    black: '#282828', red: '#ee5396', green: '#00ff74', yellow: '#08bdba',
    blue: '#78a9ff', magenta: '#be95ff', cyan: '#33b1ff', white: '#dfdfe0',
    brightBlack: '#484848', brightRed: '#f16da6', brightGreen: '#46c880', brightYellow: '#2dc7c4',
    brightBlue: '#8cb6ff', brightMagenta: '#c8a5ff', brightCyan: '#52bdff', brightWhite: '#e4e4e5',
};

const isMac = /Mac|iPhone|iPad/.test(navigator.userAgent);
// Mac shortcuts use ⌘; elsewhere Ctrl combinations belong to the programs in the terminal.
const isShortcut = (e) => (isMac ? e.metaKey && !e.ctrlKey && !e.altKey : e.ctrlKey && e.shiftKey);

const clampFontSize = (size) => (Number.isFinite(size) ? Math.min(40, Math.max(8, Math.round(size))) : DEFAULT_FONT_SIZE);

function loadFontSize() {
    try {
        return clampFontSize(JSON.parse(localStorage.getItem(PREFS_KEY) || '{}').fontSize);
    } catch {
        return DEFAULT_FONT_SIZE;
    }
}

function saveFontSize(fontSize) {
    try {
        localStorage.setItem(PREFS_KEY, JSON.stringify({fontSize}));
    } catch {
        // Not stored: the size only lasts for this page.
    }
}

const notice = (text) => `\r\n\x1b[0m\x1b[2m[${text}]\x1b[0m\r\n`;

const host = document.getElementById('terminal');
const pageTitle = document.title;

// ghostty-web 0.4.0 cancels every beforeinput event of the terminal. Keys and
// input-method composition come through other events, but text inserted
// directly was lost: macOS dictation, the emoji picker, phone keyboards. It is
// passed on here, except text an input method has just committed, which
// ghostty-web has already sent.
function acceptInsertedText(term, element) {
    let committed = null;
    element.addEventListener('compositionend', (e) => {
        committed = {data: e.data, at: performance.now()};
    });
    element.addEventListener('beforeinput', (e) => {
        if (e.inputType !== 'insertText' || e.isComposing || !e.data) return;
        if (committed && committed.data === e.data && performance.now() - committed.at < 100) return;
        term.input(e.data, true);
    });
}

await init();
// Measure the font only once it is available, or the cells get the fallback's width.
await document.fonts.load(`16px ${FONT_FAMILY}`).catch(() => {});

const term = new Terminal({
    fontFamily: FONT_FAMILY,
    fontSize: loadFontSize(),
    cursorStyle: 'bar',
    cursorBlink: true,
    scrollback: SCROLLBACK_BYTES,
    theme: THEME,
});
const fit = new FitAddon();
term.loadAddon(fit);
term.open(host);
acceptInsertedText(term, host);

// FitAddon.observeResize() ignores size changes during the 50 ms after a fit,
// so the last one of a window drag could be lost: the terminal is resized
// here, once per frame, to whatever fits.
function refit() {
    const size = fit.proposeDimensions();
    if (size && (size.cols !== term.cols || size.rows !== term.rows)) term.resize(size.cols, size.rows);
}
let frame = 0;
new ResizeObserver(() => {
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(refit);
}).observe(host);
refit();
term.focus();

// ghostty-web jumps to the bottom on every write. As in Ghostty, the view now
// stays where the user scrolled while output arrives (tail -f, ping...), and
// typing goes back to the bottom.
function writeOutput(text) {
    // Empty when a message held only the start of a character. ghostty-web
    // 0.4.0 throws on empty writes (its zero-byte allocation is out of bounds).
    if (!text) return;
    const offset = term.getViewportY();
    const before = term.getScrollbackLength();
    term.write(text);
    if (offset > 0) term.scrollToLine(offset + term.getScrollbackLength() - before);
}

let ws = null;
let state = 'connecting';
const encoder = new TextEncoder();
// Output is decoded here, keeping a character cut between two messages for the
// next one: ghostty-web only notices title changes in text, not in bytes.
let decoder = new TextDecoder();

const sendJSON = (message) => {
    if (state === 'connected') ws.send(JSON.stringify(message));
};
const sendResize = () => sendJSON({type: 'resize', data: {rows: term.rows, cols: term.cols}});

function connect() {
    state = 'connecting';
    const url = new URL('/admin/ws/terminal', location.href);
    url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const socket = new WebSocket(url);
    socket.binaryType = 'arraybuffer';
    ws = socket;
    decoder = new TextDecoder();

    socket.onopen = () => {
        state = 'connected';
        sendResize();
    };
    socket.onmessage = (event) => {
        // Output comes as binary messages, split anywhere.
        if (event.data instanceof ArrayBuffer) {
            writeOutput(decoder.decode(event.data, {stream: true}));
            return;
        }
        // Text messages are JSON control replies, or an error in plain text.
        let message = null;
        try {
            message = JSON.parse(event.data);
        } catch {
            term.write(notice(event.data));
            return;
        }
        if (message.type === 'error') term.write(notice(`出错：${message.data}`));
    };
    socket.onclose = () => {
        if (ws !== socket) return;
        const wasConnected = state === 'connected';
        state = 'closed';
        term.write(notice(`${wasConnected ? '连接已断开' : '连接失败'} · 按 Enter 重新连接`));
        document.title = pageTitle;
    };
}

term.onData((data) => {
    if (state === 'closed') {
        if (data === '\r') {
            term.write(notice('正在重新连接…'));
            connect();
        }
        return;
    }
    if (term.getViewportY() > 0) term.scrollToBottom();
    if (state === 'connected') ws.send(encoder.encode(data));
});
term.onResize(sendResize);
term.onTitleChange((title) => {
    document.title = title || pageTitle;
});

// Find (⌘F)

const searchBox = document.getElementById('search');
const searchInput = searchBox.querySelector('input');
const searchCount = searchBox.querySelector('output');
const search = createSearch(term);

function find(forward) {
    const query = searchInput.value;
    if (!query) {
        search.reset();
        searchCount.value = '';
        return;
    }
    const {index, count} = search.step(query, forward);
    searchCount.value = count ? `${index + 1}/${count}` : '无结果';
}

function openSearch() {
    const reopened = !searchBox.classList.contains('open');
    searchBox.classList.add('open');
    searchInput.select();
    // Opening again shows the last search's match.
    if (reopened && searchInput.value) find(false);
}

function closeSearch() {
    searchBox.classList.remove('open');
    search.reset();
    searchCount.value = '';
    term.focus();
}

searchInput.addEventListener('input', () => {
    search.reset();
    find(false);
});
searchInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
        e.preventDefault();
        // Enter goes up (newer output is below, the closest match first), Shift+Enter down.
        find(e.shiftKey);
    } else if (e.key === 'Escape') {
        e.preventDefault();
        closeSearch();
    }
});
searchBox.addEventListener('click', (e) => {
    const action = e.target.closest('button')?.dataset.action;
    if (action === 'previous') find(false);
    else if (action === 'next') find(true);
    else if (action === 'close') closeSearch();
});

// Shortcuts. ghostty-web stops a key when the handler returns true (xterm.js: false).

function changeFontSize(delta) {
    const size = delta === 0 ? DEFAULT_FONT_SIZE : clampFontSize(term.options.fontSize + delta);
    term.options.fontSize = size;
    saveFontSize(size);
    refit();
}

term.attachCustomKeyEventHandler((e) => {
    if (e.type !== 'keydown' || !isShortcut(e)) return false;
    // e.code: with Shift held, e.key turns into "+", "_" or ")".
    const action = {
        KeyF: openSearch,
        Equal: () => changeFontSize(1),
        NumpadAdd: () => changeFontSize(1),
        Minus: () => changeFontSize(-1),
        NumpadSubtract: () => changeFontSize(-1),
        Digit0: () => changeFontSize(0),
        Numpad0: () => changeFontSize(0),
    }[e.code];
    if (!action) return false;
    action();
    return true;
});

connect();
