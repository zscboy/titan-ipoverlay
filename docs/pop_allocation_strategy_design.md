# PoP 分配策略动态管理设计方案 (Redis Set 优化版)

## 1. 核心思路
将 PoP 分配策略从原来的静态 `config.yaml` 迁移至 Redis 持久化存储。针对大规模（1000+）PoP 节点的场景，方案采用 Redis **Set（集合）** 来存储每个区域或厂商名下的 PoP ID 列表，以支持高效的增量更新与批量拉取。

## 2. Redis 存储结构设计

### 2.1 全局基础配置 (Basic Config)
*   **Redis Key**: `titan:manager:strategy:basic`
*   **结构**: Hash
*   **字段定义**:
    *   `default_pop_id`: 默认分配的 PoP 节点 ID。
    *   `blacklist_pop_id`: 黑名单节点分配到的 PoP ID。

### 2.2 区域分配规则 (Region Rules)
为了支持大规模节点和高效增删，采用“索引+成员”的结构：
*   **区域索引 (Index)**: 
    *   **Key**: `titan:manager:strategy:regions` (Set)
    *   **内容**: `["CN", "US", "GB", ...]` (存入所有配置了规则的国家代码)
*   **区域成员 (Members)**:
    *   **Key 格式**: `titan:manager:strategy:region:{CountryCode}:members` (Set)
    *   **示例**: `titan:manager:strategy:region:CN:members` -> `["pop-sh-01", "pop-bj-02", ...]`

### 2.3 厂商分配规则 (Vendor Rules)
*   **厂商索引 (Index)**:
    *   **Key**: `titan:manager:strategy:vendors` (Set)
    *   **内容**: `["Xiaomi", "Huawei", ...]` (存入所有配置了规则的厂商名称)
*   **对应厂商成员 (Members)**:
    *   **Key 格式**: `titan:manager:strategy:vendor:{VendorName}:members` (Set)
    *   **示例**: `titan:manager:strategy:vendor:Xiaomi:members` -> `["pop-xm-01", "pop-xm-02", ...]`

---

## 3. Manager 内存同步机制 (单实例优化)

### 3.1 启动初始化 (Init/Load)
Manager 启动阶段：
1.  **加载基础**: 从 `titan:manager:strategy:basic` 读取全局变量。
2.  **加载区域**: 
    *   先获取索引：`SMEMBERS titan:manager:strategy:regions`。
    *   按索引并行获取成员：对每个 `CountryCode` 执行 `SMEMBERS titan:manager:strategy:region:{Code}:members`。
3.  **加载厂商**: 同理。
4.  **默认值回退**: 如果 Redis 为空，则从 `config.yaml` 读取初始规则集并同步入库。

### 3.2 增量/全量更新 API (Update API)

*   **同步修改接口 (Overwrite)**: Web 端若点击“保存整个列表”，Manager 执行：
    1.  `DEL titan:manager:strategy:region:CN:members` (清空旧列表)。
    2.  `SADD titan:manager:strategy:region:CN:members [new_list]` (写入新列表)。
    3.  `SADD titan:manager:strategy:regions "CN"` (确保索引存在)。
    4.  最后更新 Manager 内存中的 `RegionMap["CN"]`。
*   **单点维护接口 (Add/Remove)**: 支持通过 API 仅添加/移除一个 PoP。

---

## 4. 方案优势总结
*   **命名一致性**: 统一采用 `Index` + `Members` 的命名模式。
*   **高性能**: Redis Set 操作平均复杂度为 O(1)，量级（1000+）加载极快。
*   **配置解耦**: 区域策略与厂商策略独立存储，互不干扰。
