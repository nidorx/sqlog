## WIP!

<br>
<div align="center">
    <img src="./docs/logo.png" />
    <p align="center">
        <strong>SQLog</strong> - Connecting the dots
    </p>    
</div>

**SQLog** is a **Golang** library that leverages the power of **slog** to streamline log management in a simple and efficient way. 

One of its key advantages is its integration with **SQLite**, an embedded database, meaning there's no need for an external database server setup. This reduces operational costs and drastically simplifies system maintenance, as everything runs **embedded**, without complex dependencies.

With **SQLog**, you get a lightweight and robust solution for securely logging and storing data. It's ideal for developers looking for a practical, cost-effective, and reliable way to monitor their applications, without sacrificing performance.

## How it works

<div align="center">
    <img src="./docs/diagram.png" />
</div>

**SQLog** is an efficient library for capturing and managing logs, designed with a focus on performance and low latency. The architecture of the solution consists of four main components: **Handler**, **Ingester**, **Chunk**, and **Storage**. Each of these components plays a crucial role in the ingestion, storage, and retrieval of logs.

1. **Handler**

    The **Handler** is an implementation of `slog.Handler`, allowing **SQLog** to be easily integrated as the standard logger in Go. It transforms logs coming from `slog` into JSON format using the default encoder `slog.NewJSONHandler` or a custom encoder, providing flexibility in log formatting.

2. **Ingester**

    The **Ingester** manages the ingestion of logs from the **Handler** in a non-blocking manner. It maintains an in-memory buffer that stores log entries in blocks called **Chunks**. When a **Chunk** reaches its maximum capacity, the Ingester starts using the next **Chunk**, marking the current one for writing to **Storage**. At regular intervals, the Ingester invokes the `Flush` method of the **Storage** to persist filled **Chunks**.

    > **Performance**: One notable feature of the Ingester is its non-blocking implementation, which avoids using mutexes to ensure concurrency. Instead, it utilizes atomic operations from Go (`sync/atomic`), allowing multiple goroutines to write logs simultaneously without waiting on each other, resulting in superior performance and reduced latency.

3. **Chunk**

    The **Chunk** is a non-blocking structure that allows continuous writing of log entries. Each **Chunk** is a node in a linked list that has a reference to the next **Chunk**. The Ingester maintains references to two nodes: the current **Chunk**, which is still accepting new entries, and the flush **Chunk**, which contains completed entries that have not yet been persisted.

    > **Data Structure**: This linked list approach not only simplifies the management of **Chunks** but also facilitates fast, non-blocking writing. By updating the internal references during the flush process, the Ingester ensures that logs are written efficiently with minimal performance impact.

4. **Storage**

    The **Storage** is responsible for the persistence of logs. **SQLog**'s modular architecture allows for the implementation of different types of storage, such as disk files, databases, and external systems. Currently, there is a [native implementation for SQLite (see more details)](./sqlite) and another in development for in-memory persistence.

    **Responsibilities**:
    - **Persistence**: The Storage receives and stores **Chunks** from the Ingester.
    - **Search**: It also allows for the retrieval of records, which is used by the **SQLog** API for log visualization.

The combination of these layers makes **SQLog** a robust and efficient solution for log management, optimizing performance through a non-blocking architecture and the use of atomic operations. This results in fast, real-time log capture capable of handling high workloads without compromising efficiency.


## Features

- a
- b

## Requirements

- a
- b

## Usage

- a
- b

## @TODO

- NOT, REGEX (https://www.sqlite.org/lang_expr.html)
- Alerts
- Metrics, Count, Dashboards


Float vs Real
- https://go.dev/ref/spec#Floating-point_literals
- https://www.sqlite.org/lang_expr.html#cast_expressions
