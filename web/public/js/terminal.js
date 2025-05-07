(function () {
    // 终端配置
    var terminalSettings = {
        screenKeys: true,
        useStyle: true,
        cursorBlink: true,
        cursorStyle: 'bar', // 光标样式
        fullscreenWin: true,
        maximizeWin: true,
        screenReaderMode: true,
        cols: 128,
        allowTransparency: true,
        convertEol: true,
        disableStdin: false, // 确保允许输入
        fontFamily: 'Menlo, Monaco, "Courier New", monospace',
        fontSize: 14,
        theme: {
            foreground: 'white', // 字体
            background: '#2A2C34', // 背景色
            cursor: 'help', // 设置光标
            lineHeight: 1.2,
        },
    };

    // 终端实例变量
    var terminal, fitAddon, unicode11Addon;

    // WebSocket连接
    var ws = null;

    // MQTT连接变量
    var mqttClient = null;
    var mqttSessionID = null;
    var mqttAgentUUID = null;
    var mqttOutputTopic = null;
    var mqttInputTopic = null;

    // 通信模式：'ws'或'mqtt'
    var communicationMode = 'ws';

    // Ping间隔
    var pingInterval = null;

    // 初始化函数
    function initialize() {
        // 确保DOM元素已经有正确的背景色
        var terminalElement = document.getElementById("terminal");
        terminalElement.style.backgroundColor = "#2A2C34";
        
        // 创建终端实例
        terminal = new Terminal(terminalSettings);

        // 初始化扩展
        fitAddon = new FitAddon.FitAddon();
        unicode11Addon = new Unicode11Addon.Unicode11Addon();

        // 加载扩展
        terminal.loadAddon(fitAddon);
        terminal.loadAddon(unicode11Addon);

        // 打开终端
        terminal.open(terminalElement);
        
        // 立即适应大小
        fitAddon.fit();
        
        // 显示终端元素并隐藏加载指示器
        terminalElement.style.display = 'block';
        var loadingIndicator = document.getElementById('loading-indicator');
        if (loadingIndicator) {
            loadingIndicator.style.display = 'none';
        }

        // 处理窗口调整大小
        window.onresize = function () {
            fitAddon.fit();
        };

        // 处理终端调整大小
        terminal.onResize(function (size) {
            sendResizeCommand(size.rows, size.cols);
        });

        // 处理用户输入，特别处理Ctrl+C
        terminal.onData(function (data) {
            // 检查是否是Ctrl+C (ASCII值 3, '\x03')
            if (data.charCodeAt(0) === 3) {
                console.log('Detected Ctrl+C, sending interrupt signal');
                
                // 同时使用两种方式发送中断信号，提高成功率
                
                // 1. 作为控制消息发送中断命令
                if (communicationMode === 'mqtt' && mqttClient && mqttClient.isConnected()) {
                    // MQTT模式，只能通过数据通道发送
                    sendMQTTCommand('input', mqttSessionID, '\x03');
                } else if (communicationMode === 'ws' && ws && ws.readyState === WebSocket.OPEN) {
                    // 通过控制消息发送中断信号
                    ws.send(JSON.stringify({
                        type: 'interrupt'
                    }));
                    
                    // 2. 同时通过标准方式发送Ctrl+C字符
                    var buffer = new Uint8Array(1);
                    buffer[0] = 3; // Ctrl+C的ASCII码
                    ws.send(buffer);
                }
                
                // 在终端中显示^C以提供视觉反馈
                terminal.write('^C\r\n');
            } else {
                sendInputCommand(data);
            }
        });

        // 获取通信模式和代理UUID
        detectCommunicationMode();
    }

    // 检测通信模式和代理可用性
    function detectCommunicationMode() {
        // 从页面数据中获取模式和代理UUID
        var mode = document.getElementById('terminal-mode');
        var agent = document.getElementById('agent-uuid');

        if (mode && mode.value) {
            communicationMode = mode.value;
        }

        if (agent && agent.value) {
            mqttAgentUUID = agent.value;
        }

        // 也可以从URL参数中获取
        var urlParams = new URLSearchParams(window.location.search);
        if (urlParams.has('mode')) {
            communicationMode = urlParams.get('mode');
        }

        if (urlParams.has('agent')) {
            mqttAgentUUID = urlParams.get('agent');
        }

        // 如果没有指定模式，查询API获取建议的模式
        if (!communicationMode || (communicationMode !== 'ws' && communicationMode !== 'mqtt')) {
            // 发起AJAX请求获取终端信息
            var xhr = new XMLHttpRequest();
            xhr.open('GET', '/admin/api/terminal/info' + (mqttAgentUUID ? '?agent=' + mqttAgentUUID : ''), true);
            xhr.onreadystatechange = function () {
                if (xhr.readyState === 4) {
                    if (xhr.status === 200) {
                        var response = JSON.parse(xhr.responseText);
                        communicationMode = response.mode;
                        mqttAgentUUID = response.agentUUID;

                        // 连接终端
                        connectTerminal();
                    } else {
                        // 出错时默认使用WebSocket模式
                        communicationMode = 'ws';
                        connectTerminal();
                    }
                }
            };
            xhr.send();
        } else {
            // 已知模式，直接连接
            connectTerminal();
        }
    }

    // 连接终端
    function connectTerminal() {
        terminal.focus();

        if (communicationMode === 'mqtt') {
            // 使用MQTT模式
            connectMQTTTerminal();
        } else {
            // 默认使用WebSocket模式
            connectWebSocketTerminal();
        }
    }

    // 连接WebSocket终端
    function connectWebSocketTerminal() {
        // 清理之前的连接
        cleanupConnections();

        console.log('Connecting to WebSocket terminal...');
        
        // 建立WebSocket连接
        var protocol = (location.protocol === "https:") ? "wss://" : "ws://";
        var urlParams = mqttAgentUUID ? '?agent=' + mqttAgentUUID : '';
        var url = protocol + location.host + "/admin/ws/terminal" + urlParams;
        ws = new WebSocket(url);

        // 处理WebSocket事件
        ws.onopen = function () {
            console.log('WebSocket connection established');
            
            // 适应终端大小
            fitAddon.fit();
            // Shell 环境已在服务器端配置，无需在前端发送初始化命令

            // 启动保活ping
            pingInterval = setInterval(function () {
                if (ws && ws.readyState === WebSocket.OPEN) {
                    ws.send(JSON.stringify({
                        type: 'ping'
                    }));
                }
            }, 30000);
        };

        ws.onclose = function (event) {
            console.log('WebSocket connection closed:', event);
            terminal.write('\r\n\nConnection has been terminated. Please refresh to restart.\r\n');

            cleanupConnections();
        };

        ws.onerror = function (error) {
            console.error('WebSocket error:', error);
            terminal.write('\r\n\nError: ' + (error.message || 'WebSocket connection error') + '\r\n');
        };

        ws.onmessage = function (event) {
            // 检查消息类型
            if (event.data instanceof ArrayBuffer || event.data instanceof Blob) {
                // 处理二进制数据（终端输出）
                handleBinaryData(event.data);
            } else {
                // 处理文本数据（控制消息）
                handleWebSocketTextMessage(event.data);
            }
        };
    }

    // 连接MQTT终端
    function connectMQTTTerminal() {
        // 清理之前的连接
        cleanupConnections();

        console.log('Connecting to MQTT terminal...');
        terminal.write('Connecting to MQTT terminal...\r\n');

        // 获取MQTT连接信息
        var xhr = new XMLHttpRequest();
        xhr.open('GET', '/admin/api/terminal/mqtt/connect' + (mqttAgentUUID ? '?agent=' + mqttAgentUUID : ''), true);
        xhr.onreadystatechange = function () {
            if (xhr.readyState === 4) {
                if (xhr.status === 200) {
                    var info = JSON.parse(xhr.responseText);

                    if (!info.available) {
                        terminal.write('\r\n\nError: Agent is not available\r\n');
                        return;
                    }

                    // 设置MQTT会话信息
                    mqttSessionID = info.sessionID;
                    mqttAgentUUID = info.agentUUID;

                    // 连接MQTT
                    if (typeof mqtt !== 'undefined') {
                        connectMQTT(info);
                    } else {
                        // 如果MQTT库未加载，加载它
                        loadScript('https://cdnjs.cloudflare.com/ajax/libs/paho-mqtt/1.0.1/mqttws31.min.js', function () {
                            connectMQTT(info);
                        });
                    }
                } else {
                    terminal.write('\r\n\nError: Failed to get MQTT connection info\r\n');
                }
            }
        };
        xhr.send();
    }

    // 连接MQTT代理
    function connectMQTT(info) {
        // 准备MQTT主题
        mqttInputTopic = 'uranus/command/' + info.agentUUID;
        mqttOutputTopic = 'uranus/response/' + info.agentUUID;

        // 创建唯一的客户端ID
        var clientID = 'web_terminal_' + Math.random().toString(16).substr(2, 8);

        try {
            // 连接MQTT代理
            mqttClient = new Paho.MQTT.Client(
                info.mqttBroker, // MQTT代理地址
                clientID // 客户端ID
            );

            // 设置回调
            mqttClient.onConnectionLost = onMQTTConnectionLost;
            mqttClient.onMessageArrived = onMQTTMessageArrived;

            // 连接选项
            var options = {
                useSSL: location.protocol === 'https:',
                onSuccess: onMQTTConnect,
                onFailure: onMQTTConnectFailure
            };

            // 连接
            mqttClient.connect(options);

        } catch (e) {
            console.error('MQTT connection error:', e);
            terminal.write('\r\n\nError connecting to MQTT: ' + e.message + '\r\n');
        }
    }

    // MQTT连接成功回调
    function onMQTTConnect() {
        console.log('MQTT connected');
        
        // 订阅响应主题
        mqttClient.subscribe(mqttOutputTopic);

        // 创建终端会话
        sendMQTTCommand('create', mqttSessionID, '');

        // 适应终端大小
        fitAddon.fit();
        
        // Shell 环境已在服务器端配置，无需在前端发送初始化命令

        // 启动保活ping
        pingInterval = setInterval(function () {
            if (mqttClient && mqttClient.isConnected()) {
                sendMQTTCommand('ping', mqttSessionID, '');
            }
        }, 30000);
    }

    // MQTT连接失败回调
    function onMQTTConnectFailure(error) {
        console.error('MQTT connection failed:', error);
        terminal.write('\r\n\nFailed to connect to MQTT: ' + error.errorMessage + '\r\n');
    }

    // MQTT连接丢失回调
    function onMQTTConnectionLost(response) {
        console.log('MQTT connection lost:', response);
        terminal.write('\r\n\nMQTT connection lost: ' + response.errorMessage + '\r\n');

        cleanupConnections();
    }

    // MQTT消息接收回调
    function onMQTTMessageArrived(message) {
        // 处理MQTT消息
        try {
            var payload = JSON.parse(message.payloadString);

            // 检查会话ID是否匹配
            if (payload.sessionId && payload.sessionId === mqttSessionID) {
                if (payload.type === 'output' && payload.data) {
                    // 终端输出
                    terminal.write(payload.data);
                } else if (payload.type === 'error') {
                    // 错误消息
                    terminal.write('\r\n\nError: ' + payload.message + '\r\n');
                } else if (payload.type === 'closed') {
                    // 会话关闭
                    terminal.write('\r\n\nTerminal session closed\r\n');
                    cleanupConnections();
                }
            }
        } catch (e) {
            console.error('Error processing MQTT message:', e);
        }
    }

    // 发送MQTT命令
    function sendMQTTCommand(type, sessionID, data) {
        if (!mqttClient || !mqttClient.isConnected()) {
            return;
        }

        var command = {
            command: 'terminal',
            type: type,
            sessionId: sessionID,
            data: data,
            requestId: 'req_' + Math.random().toString(16).substr(2, 8),
            clientId: 'web_terminal'
        };

        var message = new Paho.MQTT.Message(JSON.stringify(command));
        message.destinationName = mqttInputTopic;
        message.qos = 1;

        mqttClient.send(message);
    }

    // 处理WebSocket文本消息
    function handleWebSocketTextMessage(data) {
        try {
            var message = JSON.parse(data);

            switch (message.type) {
                case 'pong':
                    // 收到pong，连接保持活跃
                    console.debug('Pong received from server');
                    break;

                case 'error':
                    // 处理错误消息
                    console.error('Terminal error:', message.data);
                    terminal.write('\r\n\nError: ' + message.data + '\r\n');
                    break;

                default:
                    console.log('Unknown message type:', message.type, message);
            }
        } catch (e) {
            console.error('Failed to parse message:', e);
            terminal.write(data);
        }
    }

    // 处理二进制数据
    function handleBinaryData(data) {
        // 将ArrayBuffer转换为字符串
        if (data instanceof ArrayBuffer) {
            var decoder = new TextDecoder();
            terminal.write(decoder.decode(data));
        } else if (data instanceof Blob) {
            // 处理Blob数据
            var reader = new FileReader();
            reader.onload = function () {
                terminal.write(new Uint8Array(reader.result));
            };
            reader.readAsArrayBuffer(data);
        }
    }

    // 发送调整大小命令
    function sendResizeCommand(rows, cols) {
        if (communicationMode === 'mqtt' && mqttClient && mqttClient.isConnected()) {
            // MQTT模式
            sendMQTTCommand('resize', mqttSessionID, {
                rows: rows,
                cols: cols
            });
        } else if (communicationMode === 'ws' && ws && ws.readyState === WebSocket.OPEN) {
            // WebSocket模式
            ws.send(JSON.stringify({
                type: 'resize',
                data: {
                    rows: rows,
                    cols: cols
                }
            }));
        }
    }

    // 发送输入命令
    function sendInputCommand(data) {
        if (communicationMode === 'mqtt' && mqttClient && mqttClient.isConnected()) {
            // MQTT模式
            sendMQTTCommand('input', mqttSessionID, data);
        } else if (communicationMode === 'ws' && ws && ws.readyState === WebSocket.OPEN) {
            // WebSocket模式
            var encoder = new TextEncoder();
            var uint8Array = encoder.encode(data);
            ws.send(uint8Array);
        }
    }

    // Shell 环境配置已移至服务器端

    // 清理连接
    function cleanupConnections() {
        // 清理WebSocket连接
        if (ws) {
            if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
                ws.close();
            }
            ws = null;
        }

        // 清理MQTT连接
        if (mqttClient && mqttClient.isConnected()) {
            // 关闭会话
            if (mqttSessionID) {
                sendMQTTCommand('close', mqttSessionID, '');
            }

            // 断开MQTT连接
            try {
                mqttClient.disconnect();
            } catch (e) {
                console.error('Error disconnecting MQTT:', e);
            }
            mqttClient = null;
        }

        // 清理ping间隔
        if (pingInterval) {
            clearInterval(pingInterval);
            pingInterval = null;
        }
    }

    // 加载外部脚本
    function loadScript(url, callback) {
        var script = document.createElement('script');
        script.type = 'text/javascript';
        script.src = url;
        script.onload = callback;
        document.head.appendChild(script);
    }

    // 初始化
    initialize();

    // 在窗口关闭前清理连接
    window.addEventListener('beforeunload', function () {
        cleanupConnections();
    });
})();
