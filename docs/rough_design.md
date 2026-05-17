# **Technical Design Document: Kubernetes eBPF Network Observability**

**Date:** January 3, 2026  
**Status:** Draft  
**Authors:** Engineering Team

## **1\. Executive Summary**

This document outlines the architecture for a distributed monitoring solution designed to capture TCP and HTTP metrics for Kubernetes workloads without requiring application code modification. The system utilizes **eBPF (Extended Berkeley Packet Filter)** for low-overhead, kernel-level instrumentation.  
The solution consists of a **Custom Resource Definition (CRD)** for defining monitoring intent, a **Controller** for configuration management, and a **DaemonSet Agent** utilizing the OpenTelemetry eBPF instrumentation library to capture and export Prometheus metrics.

## **2\. System Architecture**

The system follows a standard Kubernetes Operator pattern with a control-plane / data-plane separation.

### **2.1 High-Level Components**

1. **TrafficMonitor CRD:** The user-facing API to define *what* to monitor (Target Pods via labels) and *how* (HTTP/TCP, Ports).  
2. **Config Controller:** A centralized operator that watches TrafficMonitor resources and translates high-level intents into configurations for the agents.  
3. **eBPF Agent (DaemonSet):** A privileged node agent that receives configuration, attaches eBPF probes via the OpenTelemetry library, and exposes a Prometheus /metrics endpoint.

## **3\. API Design (Custom Resource)**

We will introduce a Cluster-scoped CRD named TrafficMonitor.

### **3.1 Schema Definition**

YAML  
apiVersion: monitoring.example.com/v1alpha1  
kind: TrafficMonitor  
metadata:  
  name: payment-service-monitor  
spec:  
  \# Select pods to monitor  
  workloadSelector:  
    matchLabels:  
      app: payment-service  
    
  \# Protocol definitions  
  protocols:  
    \- type: HTTP  
      ports: \[8080\]  
      settings:  
        captureHeaders: false  
    \- type: TCP  
      ports: \[5432\]  
    
  \# Metric collection settings  
  metrics:  
    interval: 10s \# Aggregation window

## **4\. Component Design**

### **4.1 The Controller (Control Plane)**

* **Reconciliation:** Watches TrafficMonitor resources.  
* **Service Discovery:** Bridges the gap between abstract "Labels" and concrete "Processes/IPs".  
* **Configuration Distribution:** The Controller maintains a configuration state (via ConfigMap or gRPC) that Agents watch. It resolves Label Selectors to pass specific monitoring rules to the Agents.

### **4.2 The Agent (Data Plane)**

* **Local Discovery:** Queries the local Kubelet to find Pods on *its specific node* that match active policies.  
* **eBPF Instrumentation:** Uses open-telemetry/opentelemetry-ebpf-instrumentation to attach probes.  
* **Metric Aggregation:** Buffers events from kernel space and aggregates them into Prometheus counters/histograms.

#### **4.2.1 eBPF Implementation Details**

* **Library:** We will utilize open-telemetry/opentelemetry-ebpf-instrumentation for uProbes (HTTP/gRPC) and kProbes (TCP).  
* **Filtering:** The Agent will maintain a map of {PID \-\> TrafficMonitorRule}. When an event triggers (e.g., sys\_enter\_connect), the eBPF program checks if the PID belongs to a monitored Pod to ensure low overhead.

### **4.3 Metrics Specification**

The Agent exposes metrics at :9090/metrics.  
**HTTP Metrics:**

* workload\_http\_requests\_total (Counter): Total requests processed. Labels: method, status\_code, pod.  
* workload\_http\_duration\_seconds (Histogram): Latency distribution.

**TCP Metrics:**

* workload\_tcp\_bytes\_total (Counter): Volume transferred. Labels: direction (tx/rx).  
* workload\_tcp\_rtt\_seconds (Gauge): Round Trip Time (latency).  
* workload\_tcp\_retransmits\_total (Counter): Network quality indicator.

## **5\. Data Flow**

1. **User** creates TrafficMonitor CR targeting app: frontend.  
2. **Controller** validates and broadcasts the intent to Agents.  
3. **Agent (Node Level)** receives update: "Monitor app: frontend".  
4. **Agent** resolves local frontend pods to PIDs (e.g., PID 12345).  
5. **Agent** updates eBPF map to whitelist PID 12345\.  
6. **Kernel** triggers eBPF program on network activity for that PID, capturing metrics.  
7. **Agent** aggregates data and updates the Prometheus registry.

## **6\. Development Prerequisites**

* **Language:** Go (Golang) 1.22+  
* **Libraries:** controller-runtime, cilium/ebpf, open-telemetry/opentelemetry-ebpf-instrumentation.  
* **Environment:** Linux Kernel 5.4+ (BTF/CO-RE support required).

## **7\. Security Considerations**

* **Privileges:** DaemonSet requires CAP\_SYS\_ADMIN and CAP\_BPF.  
* **Resource Limits:** Strict CPU/Memory limits will be enforced to prevent the agent from impacting workload performance.

### **Next Step**

Would you like me to start writing the **Go struct definitions** for the TrafficMonitor API so you can begin the implementation?