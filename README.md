## Nginx Proxy Manager (NPM)

Gin 框架写的 NGINX 管理工具，包括自升级


自动读取NGINX配置文件， 并且可以在线操作，目的是可以随时添加站点，取代宝塔面板。

至于怎么用，自用，暂时不出文档

### 功能特性

* SSL 自动更新 : [Lego](https://github.com/go-acme/lego)
* 平滑更新支持 : [cloudflare/tableflip](https://github.com/cloudflare/tableflip)
* 数据库支持 : [gorm](https://github.com/go-gorm/gorm)
* SQLite : [SQLite](https://github.com/go-gorm/sqlite)

### 一键脚本

```bash
# 目前只测试过 ubuntu 20.04
wget -qO- https://fr.qfdk.me/install.sh|bash
# 自动杀死进程
kill -9 $(ps aux|grep "nginx-proxy-manager"|grep -v grep|awk '{print $2}')
```

### 截图预览
![dashboard](https://s2.loli.net/2022/04/15/vsWiH5YdnQrlhaB.png)