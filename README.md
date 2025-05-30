## Οὐρανός (拉丁语：Uranus)

Gin 框架写的 nginx 图形界面管理程序，可以 增、删、改、查 nginx的所有配置。
之前使用宝塔面板出现了很多未解之谜，比如为啥自动升级这样的问题，而且强制捆绑手机号。
本程序为了完全接管 nginx，然后方便自己进行管理操作。
还可以执行命令

**自用**，暂时不出文档, 任何问题与本人无关。

### ⚠️ 隐私声明

**此为个人使用的代理程序，MQTT功能用于远程命令执行和数据上报。**

- 默认MQTT服务器: `mqtt://mqtt.qfdk.me:1883`
- **MQTT主要用途：**
  - 远程终端命令执行
  - 系统心跳和状态监控
  - 上报数据：系统信息、构建版本、IP地址、主机名等
- 如果您不希望连接到作者的MQTT服务器，请修改配置文件

**如何使用自己的MQTT服务器：**
1. 编辑 `config.toml` 文件
2. 修改 `mqttBroker` 为您自己的MQTT服务器地址

```toml
# 修改为您自己的MQTT服务器
mqttBroker = "mqtt://your-mqtt-server:1883"
```

**注意：** MQTT功能是远程管理的核心，禁用后将无法使用远程终端和集中管理功能。

### 功能特性

* SSL 自动更新 : [Lego](https://github.com/go-acme/lego)
* 平滑更新支持 : [cloudflare/tableflip](https://github.com/cloudflare/tableflip)
* 数据库支持 : [gorm](https://github.com/go-gorm/gorm)
* SQLite : [SQLite](https://github.com/go-gorm/sqlite)
* Terminal : [ttyd](https://github.com/tsl0922/ttyd)
* VSCode : [vscode](https://github.com/microsoft/vscode)
* 一键升级

### 一键脚本

```bash
# 目前只测试过 ubuntu 20.04/22.04
wget -qO- https://fr.qfdk.me/uranus/install.sh|bash
# 升级脚本
wget -qO- https://fr.qfdk.me/uranus/upgrade.sh|bash
# 杀死进程
kill -9 $(ps aux|grep "uranus"|grep -v grep|awk '{print $2}')
```

### 截图预览

![login.png](./docs/login.png)
![dashboard.png](./docs/dashboard.png)
![nginx.png](./docs/nginx_default.png)
![sites.png](./docs/sites.png)
![ssl.png](./docs/ssl.png)
![terminal.png](./docs/terminal.png)
![terminal2.png](./docs/terminal2.png)
