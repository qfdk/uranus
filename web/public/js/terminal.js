(function () {
    var terminal = new Terminal({
        screenKeys: true,
        useStyle: true,
        cursorBlink: true,
        cursorStyle: 'bar', // 光标样式
        fullscreenWin: true,
        maximizeWin: true,
        screenReaderMode: true,
        cols: 128,
        theme: {
            foreground: 'white', // 字体
            background: '#2A2C34', // 背景色
            cursor: 'help', // 设置光标
            lineHeight: 16,
        },
    });
    terminal.open(document.getElementById("terminal"));
    var protocol = (location.protocol === "https:") ? "wss://" : "ws://";
    var url = protocol + location.host + "/admin/xterm.js"
    var ws = new WebSocket(url);
    var attachAddon = new AttachAddon.AttachAddon(ws);
    var fitAddon = new FitAddon.FitAddon();
    terminal.loadAddon(fitAddon);
    var unicode11Addon = new Unicode11Addon.Unicode11Addon();
    terminal.loadAddon(unicode11Addon);
    ws.onclose = function (event) {
        console.log(event);
        terminal.write('\r\n\nconnection has been terminated from the server-side (hit refresh to restart)\n')
    };
    ws.onopen = function () {
        terminal.loadAddon(attachAddon);
        terminal._initialized = true;
        terminal.focus();
        setTimeout(function () {
            fitAddon.fit()
            ws.send("export TERM=xterm\n")
            ws.send("PS1=\"\\[\\033[01;31m\\]\\u\\[\\033[01;33m\\]@\\[\\033[01;36m\\]\\h \\[\\033[01;33m\\]\\w \\[\\033[01;35m\\]\\$ \\[\\033[00m\\]\"\n")
            ws.send("alias ls='ls --color'\n")
            ws.send("alias ll='ls -alF'\n")
            ws.send("clear\n")
        });
        terminal.onResize(function (event) {
            var rows = event.rows;
            var cols = event.cols;
            var size = JSON.stringify({cols: cols, rows: rows + 1});
            var send = new TextEncoder().encode("\x01" + size);
            console.log('resizing to', size);
            ws.send(send);
        });
        terminal.onTitleChange(function (event) {
            console.log(event);
        });
        window.onresize = function () {
            fitAddon.fit();
        };
    };
})();
