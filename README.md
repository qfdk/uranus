## Οὐρανός (拉丁语：Uranus)

Gin 框架写的 nginx 图形界面管理程序，可以 增、删、改、查 nginx的所有配置。
之前使用宝塔面板出现了很多未解之谜，比如为啥自动升级这样的问题，而且强制捆绑手机号。
本程序为了完全接管 nginx，然后方便自己进行管理操作。
增加了在线命令功能，使用了 [cloudshell](https://github.com/zephinzer/cloudshell) 部分代码

**自用**，暂时不出文档, 任何问题与本人无关。


### 功能特性

* SSL 自动更新 : [Lego](https://github.com/go-acme/lego)
* 平滑更新支持 : [cloudflare/tableflip](https://github.com/cloudflare/tableflip)
* 数据库支持 : [gorm](https://github.com/go-gorm/gorm)
* SQLite : [SQLite](https://github.com/go-gorm/sqlite)
* Terminal : [cloudshell](https://github.com/zephinzer/cloudshell)

### 一键脚本
```bash
# 目前只测试过 ubuntu 20.04
wget -qO- https://fr.qfdk.me/install.sh|bash
# 自动杀死进程
kill -9 $(ps aux|grep "uranus"|grep -v grep|awk '{print $2}')
```

### nginx-proxy-manager 转换为 uranus
```bash
systemctl stop nginx-proxy-manager
systemctl disable nginx-proxy-manager
systemctl daemon-reload
rm -rf /etc/systemd/system/nginx-proxy-manager.service
mv /etc/nginx-proxy-manager /etc/uranus
### /!\ 配置文件记得修改 /etc/nginx-proxy-manager /etc/uranus
wget -qO- https://fr.qfdk.me/install.sh|bash
```

### 截图预览
![login.png](https://s2.loli.net/2022/05/27/OCkzUoEr6F5WpSP.png)
![dashboard.png](https://s2.loli.net/2022/05/27/OwGDMal7vrTtVQ9.png)
![config.png](https://s2.loli.net/2022/05/27/xJYOSdL2fapBu3j.png)
![terminal.png](https://s2.loli.net/2022/05/27/uKNtXmMSEB7P6VF.png)
