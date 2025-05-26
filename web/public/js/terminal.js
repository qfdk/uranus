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

    // 初始化函数 - 优化加载和性能
    function initialize() {
        // 确保DOM元素已经有正确的背景色
        var terminalElement = document.getElementById("terminal");
        terminalElement.style.backgroundColor = "#2A2C34";

        try {
            // 预加载终端字体以减少FOUT (Flash of Unstyled Text)
            if ('fonts' in document) {
                // 预加载monospace字体
                document.fonts.load('1em monospace').then(function () {
                }).catch(function (err) {
                    // Font preloading failed
                });
            }

            // 创建终端实例，使用try-catch确保错误不会中断初始化
            terminal = new Terminal(terminalSettings);

            // 初始化扩展
            fitAddon = new FitAddon.FitAddon();
            unicode11Addon = new Unicode11Addon.Unicode11Addon();

            // 加载扩展
            terminal.loadAddon(fitAddon);
            terminal.loadAddon(unicode11Addon);

            // 打开终端 - 使用优化的打开流程
            terminal.open(terminalElement);

            // 设置初始焦点，提升用户体验
            setTimeout(function () {
                terminal.focus();
            }, 100);

            // 立即适应大小并触发重绘
            fitAddon.fit();
            terminal.refresh(0, terminal.rows - 1);

            // 显示终端元素并隐藏加载指示器
            terminalElement.style.display = 'block';
            var loadingIndicator = document.getElementById('loading-indicator');
            if (loadingIndicator) {
                loadingIndicator.style.display = 'none';
            }

            // 报告初始化成功
        } catch (err) {
            // 错误处理 - 尝试降级到基本终端
            console.error('Terminal initialization failed:', err);

            // 显示错误信息给用户
            var loadingIndicator = document.getElementById('loading-indicator');
            if (loadingIndicator) {
                loadingIndicator.innerHTML = '<div style="color: #ff5555; margin-bottom: 20px;">终端初始化失败<br>请刷新页面重试</div>';
            }
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

                // 在终端中显示^C以提供视觉反馈，但不包含换行符
                // 这样可以让服务器端的响应正常显示
                terminal.write('^C');
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

        terminal.write('正在连接终端服务器...\r\n');

        // 建立WebSocket连接
        var protocol = (location.protocol === "https:") ? "wss://" : "ws://";
        var urlParams = mqttAgentUUID ? '?agent=' + mqttAgentUUID : '';
        var url = protocol + location.host + "/admin/ws/terminal" + urlParams;

        // 连接超时处理
        var connectTimeout = setTimeout(function () {
            if (ws && ws.readyState !== WebSocket.OPEN) {
                terminal.write('\r\n\n连接超时，请检查网络后刷新页面重试。\r\n');
                console.error('WebSocket connection timeout');
                if (ws) {
                    ws.close();
                    ws = null;
                }
            }
        }, 10000); // 10秒连接超时

        try {
            ws = new WebSocket(url);

            // 创建重连机制
            var reconnectAttempts = 0;
            var maxReconnectAttempts = 3;
            var reconnectTimeout = null;

            // 处理WebSocket事件
            ws.onopen = function () {
                clearTimeout(connectTimeout); // 清除连接超时
                reconnectAttempts = 0; // 重置重连计数

                // 适应终端大小
                fitAddon.fit();
                // Shell 环境已在服务器端配置，无需在前端发送初始化命令

                // 发送初始终端大小
                sendResizeCommand(terminal.rows, terminal.cols);

                // 启动保活ping - 优化的版本，带错误处理
                if (pingInterval) {
                    clearInterval(pingInterval);
                }

                pingInterval = setInterval(function () {
                    if (ws && ws.readyState === WebSocket.OPEN) {
                        try {
                            ws.send(JSON.stringify({
                                type: 'ping'
                            }));
                        } catch (e) {
                            console.error('Error sending ping:', e);
                            clearInterval(pingInterval);
                        }
                    } else {
                        clearInterval(pingInterval);
                    }
                }, 30000);
            };

            ws.onclose = function (event) {
                clearTimeout(connectTimeout); // 清除连接超时

                // 自动重连函数
                function attemptReconnect() {
                    if (reconnectAttempts < maxReconnectAttempts) {
                        reconnectAttempts++;
                        terminal.write('\r\n\n连接断开，正在尝试重新连接 (' + reconnectAttempts + '/' + maxReconnectAttempts + ')...\r\n');

                        // 指数退避重连
                        var timeout = Math.min(30000, 1000 * Math.pow(2, reconnectAttempts));

                        reconnectTimeout = setTimeout(function () {
                            if (terminal) { // 确保终端仍然存在
                                connectWebSocketTerminal();
                            }
                        }, timeout);
                    } else {
                        terminal.write('\r\n\n重连失败，请刷新页面手动重连。\r\n');
                    }
                }

                // 如果不是正常关闭，尝试重连
                if (event.code !== 1000 && event.code !== 1005) {
                    attemptReconnect();
                } else {
                    terminal.write('\r\n\n连接已关闭。请刷新页面重连。\r\n');
                }

                cleanupConnections();
            };

            ws.onerror = function (error) {
                console.error('WebSocket error:', error);
                clearTimeout(connectTimeout); // 清除连接超时
                terminal.write('\r\n\n连接错误: ' + (error.message || '网络连接问题') + '\r\n');

                // 错误后不立即尝试重连，让onclose处理重连逻辑
            };
        } catch (e) {
            console.error('Failed to create WebSocket:', e);
            terminal.write('\r\n\n创建WebSocket连接失败: ' + e.message + '\r\n');
            clearTimeout(connectTimeout);
        }

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

    // 连接MQTT终端 - 增强版本
    function connectMQTTTerminal() {
        // 清理之前的连接
        cleanupConnections();

        terminal.write('正在连接MQTT终端...\r\n');

        // 连接超时处理
        var connectTimeout = setTimeout(function () {
            terminal.write('\r\n\nMQTT连接超时，请检查网络或刷新页面。\r\n');
            console.error('MQTT connection timeout');
        }, 15000); // 15秒连接超时

        // 获取MQTT连接信息，使用Promise包装以便更好地处理错误和超时
        function getMQTTInfo() {
            return new Promise(function (resolve, reject) {
                var xhr = new XMLHttpRequest();
                xhr.open('GET', '/admin/api/terminal/mqtt/connect' + (mqttAgentUUID ? '?agent=' + mqttAgentUUID : ''), true);
                xhr.timeout = 10000; // 10秒超时

                xhr.onreadystatechange = function () {
                    if (xhr.readyState === 4) {
                        if (xhr.status === 200) {
                            try {
                                var info = JSON.parse(xhr.responseText);
                                resolve(info);
                            } catch (e) {
                                reject(new Error('解析MQTT连接信息失败: ' + e.message));
                            }
                        } else {
                            reject(new Error('获取MQTT连接信息失败，服务器返回: ' + xhr.status));
                        }
                    }
                };

                xhr.ontimeout = function () {
                    reject(new Error('获取MQTT连接信息超时'));
                };

                xhr.onerror = function () {
                    reject(new Error('网络错误，无法获取MQTT连接信息'));
                };

                xhr.send();
            });
        }

        // 使用Promise处理连接流程
        getMQTTInfo()
            .then(function (info) {
                if (!info.available) {
                    throw new Error('Agent无法连接或不可用');
                }

                // 设置MQTT会话信息
                mqttSessionID = info.sessionID;
                mqttAgentUUID = info.agentUUID;

                // 先检查MQTT库是否已加载
                if (typeof Paho !== 'undefined' && Paho.MQTT) {
                    return Promise.resolve(info);
                } else {
                    // 加载MQTT库
                    return new Promise(function (resolve, reject) {
                        loadScript('https://cdnjs.cloudflare.com/ajax/libs/paho-mqtt/1.0.1/mqttws31.min.js', function () {
                            // 检查加载是否成功
                            if (typeof Paho !== 'undefined' && Paho.MQTT) {
                                resolve(info);
                            } else {
                                reject(new Error('加载MQTT库失败'));
                            }
                        });
                    });
                }
            })
            .then(function (info) {
                // 连接MQTT
                connectMQTT(info);
                clearTimeout(connectTimeout);
            })
            .catch(function (error) {
                console.error('MQTT连接过程失败:', error);
                terminal.write('\r\n\n连接失败: ' + error.message + '\r\n');
                terminal.write('请刷新页面重试或联系管理员。\r\n');
                clearTimeout(connectTimeout);
            });
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
                    break;

                case 'error':
                    // 处理错误消息
                    console.error('Terminal error:', message.data);
                    terminal.write('\r\n\nError: ' + message.data + '\r\n');
                    break;

                default:
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
                rows,
                cols
            });
        } else if (communicationMode === 'ws' && ws && ws.readyState === WebSocket.OPEN) {
            // WebSocket模式
            ws.send(JSON.stringify({
                type: 'resize',
                data: {
                    rows,
                    cols
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

    // 清理连接 - 增强版本，确保所有资源正确释放
    function cleanupConnections() {

        var cleanupPromises = [];

        // 清理WebSocket连接
        if (ws) {
            cleanupPromises.push(new Promise(function (resolve) {
                try {
                    // 如果连接仍然打开或正在连接，发送关闭消息
                    if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
                        // 先发送关闭命令，以便服务器端可以优雅关闭
                        if (ws.readyState === WebSocket.OPEN) {
                            try {
                                ws.send(JSON.stringify({
                                    type: 'terminate'
                                }));
                            } catch (e) {
                                console.warn('发送终止命令失败:', e);
                            }

                            // 设置关闭超时，确保不会卡住
                            var closeTimeout = setTimeout(function () {
                                resolve();
                            }, 2000);

                            // 监听关闭事件
                            var originalOnClose = ws.onclose;
                            ws.onclose = function (event) {
                                clearTimeout(closeTimeout);
                                if (originalOnClose) originalOnClose(event);
                                resolve();
                            };

                            // 正常关闭WebSocket
                            ws.close(1000, "Terminal cleanup");
                        } else {
                            // 连接中状态，直接关闭
                            ws.close();
                            resolve();
                        }
                    } else {
                        // 已经关闭或关闭中，直接解析
                        resolve();
                    }
                } catch (e) {
                    console.error('关闭WebSocket时发生错误:', e);
                    resolve(); // 即使出错也要继续
                } finally {
                    ws = null;
                }
            }));
        }

        // 清理MQTT连接
        if (mqttClient) {
            cleanupPromises.push(new Promise(function (resolve) {
                try {
                    if (mqttClient.isConnected && mqttClient.isConnected()) {
                        // 发送关闭会话消息
                        if (mqttSessionID) {
                            try {
                                sendMQTTCommand('close', mqttSessionID, '');
                                // 给服务器时间处理关闭命令
                                setTimeout(function () {
                                    disconnectMQTT();
                                }, 500);
                            } catch (e) {
                                console.error('发送MQTT关闭命令失败:', e);
                                disconnectMQTT();
                            }
                        } else {
                            disconnectMQTT();
                        }
                    } else {
                        resolve();
                    }

                    function disconnectMQTT() {
                        try {
                            mqttClient.disconnect();
                        } catch (e) {
                            console.error('断开MQTT连接时发生错误:', e);
                        } finally {
                            mqttClient = null;
                            resolve();
                        }
                    }
                } catch (e) {
                    console.error('清理MQTT时发生错误:', e);
                    mqttClient = null;
                    resolve();
                }
            }));
        }

        // 清理所有定时器
        if (pingInterval) {
            clearInterval(pingInterval);
            pingInterval = null;
        }

        // 取消所有可能的重连尝试
        if (typeof reconnectTimeout !== 'undefined' && reconnectTimeout) {
            clearTimeout(reconnectTimeout);
            reconnectTimeout = null;
        }

        // 等待所有清理操作完成
        Promise.all(cleanupPromises).then(function () {
        }).catch(function (error) {
            console.error('清理连接时发生错误:', error);
        });
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
