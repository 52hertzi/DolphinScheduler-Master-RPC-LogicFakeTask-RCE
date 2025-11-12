# DolphinScheduler Master RPC LogicFakeTask Remote Code Execution

## 1. Background
The Apache DolphinScheduler master process receives workflow scheduling requests, maintains execution state, and communicates with workers and the API server through an embedded Netty-based RPC framework. By default this RPC service listens on port 5678/TCP and is reachable from outside the cluster. The master ships with a set of task plugins, including `LogicFakeTask`, a testing plugin that nevertheless remains enabled in production builds. The plugin executes arbitrary shell scripts, which opens the door for remote abuse.

## 2. Vulnerability Overview
- **Type**: Unauthenticated Remote Code Execution.
- **Target**: Master RPC service (`MasterRpcServer`).
- **Root Cause**: RPC endpoints lack authentication, incoming parameters are not validated, and LogicFakeTask executes arbitrary shell scripts provided by the caller.
- **Impact**: An attacker can run any command on the master host with the privileges of the master process (typically root), obtaining full control over the scheduling platform.

## 3. Affected Scope
- **Version**: Verified on 3.3.2; any release shipping LogicFakeTask without RPC hardening is presumed vulnerable.
- **Topology**: The issue affects both single-master and multi-master clusters; any environment where port 5678 is reachable can be compromised. The risk increases in deployments where masters and workers are separated.
- **Network Exposure**: If 5678/TCP is exposed to corporate networks or the Internet, the attack surface widens considerably.

## 4. Attack Chain
1. **Reconnaissance** – The attacker scans the master host and discovers port 5678/TCP is open.
2. **Crafting the RPC Call** – The request uses the method identifier for `ILogicTaskExecutorOperator.dispatchTask`:
   ```java
   public abstract TaskExecutorDispatchResponse dispatchTask(TaskExecutorDispatchRequest request)
   ```
   `argsTypes` is set to `org.apache.dolphinscheduler.task.executor.operations.TaskExecutorDispatchRequest`, and `args` contains a forged `TaskExecutionContext` with `taskType=LogicFakeTask` and `taskParams={"shellScript":"<payload>"}`.
3. **Sending the Packet** – The payload is wrapped in the Netty Transporter frame format and written directly to the TCP socket.
4. **Unauthenticated Execution** – The master deserializes the request, instantiates LogicFakeTask, and executes `ProcessBuilder("/bin/sh", "-c", shellScript)` with the attacker-controlled script.
5. **Post-Exploitation** – The attacker can implant backdoors, harvest credentials, or pivot to workers, databases, and storage systems.

## 5. Code-Level Details
### 5.1 RPC Method Exposure
`SpringServerMethodInvokerDiscovery` registers every `@RpcService` method as an invokable handler:
```java
for (Method method : anInterface.getDeclaredMethods()) {
    RpcMethod rpcMethod = method.getAnnotation(RpcMethod.class);
    if (rpcMethod == null) {
        continue;
    }
    ServerMethodInvoker serverMethodInvoker =
        new ServerMethodInvokerImpl(serverMethodInvokerProviderBean, method);
    nettyRemotingServer.registerMethodInvoker(serverMethodInvoker);
}
```
Inside `ServerMethodInvokerImpl`, invocation occurs without any authorization checks:
```java
@Override
public Object invoke(Object... args) throws Throwable {
    try {
        return method.invoke(serviceBean, args);
    } catch (InvocationTargetException ex) {
        throw ex.getTargetException();
    }
}
```
Any client that knows the `methodIdentifier` can call privileged RPC operations directly.

### 5.2 LogicFakeTask Arbitrary Command Execution
The core of `LogicFakeTask.start()` is:
```java
String shellScript = ParameterUtils.convertParameterPlaceholders(
        taskParameters.getShellScript(),
        ParameterUtils.convert(taskExecutionContext.getPrepareParamsMap()));

if (StringUtils.isNotEmpty(taskExecutionContext.getEnvironmentConfig())) {
    shellScript = taskExecutionContext.getEnvironmentConfig() + "\n" + shellScript;
}
ProcessBuilder processBuilder = new ProcessBuilder("/bin/sh", "-c", shellScript);
processBuilder.redirectErrorStream(true);
process = processBuilder.start();
```
`taskParameters.getShellScript()` originates from the RPC request, so the attacker controls the command executed on the master node.

### 5.3 Forged TaskExecutionContext
The `TaskExecutionContext` object is reconstructed from untrusted JSON without validation:
```java
taskExecutionContext = jsonMapper.readValue(taskParamsJson, TaskExecutionContext.class);
```
As a result, the attacker can spoof workflow IDs, tenants, and task types.

## 6. Reproduction Steps
1. From a host that can reach the master RPC port, run:
   ```bash
   go run master_rpc_logic_fake_rce_poc.go \
     -target 10.1.1.10:5678 \
     -cmd 'touch /tmp/master_rpc_rce_go'
   ```
2. Expected output: `[+] LogicFakeTask dispatched successfully, command delivered: touch /tmp/master_rpc_rce_go`.
3. Verify on the master host:
   ```bash
   ls /tmp | grep master_rpc_rce_go
   ```
4. Check master logs:
   ```text
   Receive TaskExecutorDispatchRequest(... taskName=logic_fake_rpc_poc, taskType=LogicFakeTask ...)
   LogicFakeTask: logic_fake_rpc_poc execute success
   ```

## 7. Risk Assessment
- The master typically runs with high privileges, so command execution leads to full compromise.
- The attack requires no credentials and can be launched remotely.
- Once inside, the attacker can tamper with workflows, exfiltrate secrets, or pivot across the platform.

## 8. Mitigation Recommendations
1. **Network Isolation** – Immediately restrict access to 5678/TCP so that only trusted cluster nodes can reach the master.
2. **RPC Authentication** – Enforce mutual TLS, request signing, or token-based authentication; validate caller identity with the registry.
3. **Plugin Hardening** – Remove or disable test plugins such as `LogicFakeTask`; enforce allowlists for task types in production.
4. **Input Validation** – Verify `workflowInstanceId`, `tenantCode`, and `taskType` against legitimate runtime context to detect spoofed requests.
5. **Patching & Release** – Incorporate fixes in upcoming releases and publish security advisories prompting users to upgrade and harden their deployments.

## 9. Emergency and Long-Term Actions
- **Short Term** – Close the RPC port to untrusted networks and disable LogicFakeTask to reduce immediate exposure.
- **Medium Term** – Implement RPC authentication/authorization and deploy request signatures or tokens.
- **Long Term** – Adopt a secure release process that keeps testing components out of production builds and enforces security reviews.

## 10. References
- Go PoC: `master_rpc_logic_fake_rce_poc.go`
- Key source files:
  - `org.apache.dolphinscheduler.server.master.rpc.MasterRpcServer`
  - `org.apache.dolphinscheduler.server.master.engine.executor.plugin.fake.LogicFakeTask`
  - `org.apache.dolphinscheduler.extract.base.server.ServerMethodInvokerImpl`
