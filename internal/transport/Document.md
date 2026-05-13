设计流程
第一步：修改 SyncMessage，为 Handshake 增加本机时间戳字段
当前 MsgHandShake 只携带 PeerID，需要增加 Timestamp int64，表示握手发起时刻（毫秒时间戳）。这不是 UUID 中的时间，而是握手发生时的实时时间戳，用于在双方几乎同时 UUID 也可比较的场景下做二次裁决。
第二步：握手三阶段
  Dialer（主动方）                         Listener（被动方）
      │                                        │
      ├── TCP Connect ────────────────────────→│
      │                                        │
      ├── Handshake{PeerID, Timestamp} ──────→│ (阶段1: 发送)
      │                                        │
      │              Handshake{PeerID, Ts} ←───┤ (阶段2: 回复)
      │                                        │
      │  ← 双方比较，决策 →                      │ (阶段3: 裁决)
阶段1：主动连接方（Dialer）在 ConnectTo() 成功后立即发送 Handshake，携带自己的 PeerID + 握手时间戳。
阶段2：被动方（Listener）在 accessLoop 收到新连接后，先读取对方的 Handshake，然后回复自己的 Handshake。
阶段3：双方都拿到对方的 (PeerID, Timestamp) 后执行裁决。
第三步：裁决规则（Tie-Breaking）
对于每一对 (A, B)，最多存在两条连接。裁决目标：保留恰好一条连接。
规则（两级比较）：
1. 优先比较 PeerID（UUID 字典序）—— 较小者获胜
2. 如果 PeerID 中的时间戳部分相同（极端情况），比较握手 Timestamp —— 较小者获胜
胜负语义：
  - 获胜方：作为 "Server 角色"，有权保留连接
  - 落败方：作为 "Client 角色"，需服从
连接消除规则（双方各自独立执行相同的逻辑即可达成一致）：
  设 myID = 自己的PeerID,  remoteID = 对方的PeerID
  if myID < remoteID:    // 我是获胜方
      如果当前连接是"我主动发起(dial)"的 → 保留
      如果当前连接是"我被动接受(accept)"的 → 关闭（让对方保留他主动发起的）
  else:                   // 我是落败方
      如果当前连接是"我主动发起(dial)"的 → 关闭（让对方保留他被动接受的）
      如果当前连接是"我被动接受(accept)"的 → 保留
等价简化表述：每一对 peer 之间，PeerID 较小的一方总是扮演 Dialer（主动方），较大的一方总是扮演 Listener（被动方）。任何不符合此角色的连接都视为多余并被关闭。
第四步：状态管理
Transport 需要将 map[string]*net.Conn（按地址索引）改为 按 PeerID 索引：
type peerConn struct {
    conn      *net.Conn
    peerID    string
    direction string  // "dial" 或 "accept"
}
peer map[string]*peerConn  // key 从 addr 改为 PeerID
这样做的好处：
- handleHandShake 完成后，立即检查 peer 映射中是否已存在该 PeerID
- 若存在 → 触发重复连接裁决
- 若不存在 → 直接注册
第五步：超时与容错
- 握手超时：连接建立后 5 秒内未收到 Handshake → 断开
- 裁决期间短暂加锁，防止并发竞态（两条连接同时完成握手时的 race）
- 被关闭一方收到 io.EOF，正常清理资源
---
完整流程图
Peer A (ID较小)                    Peer B (ID较大)
    │                                    │
    ├── Dial ────────────────────────→ (accept)      连接1
    │   Send Handshake{A_id, A_ts}      │
    │                                    ├── 收到Handshake
    │                                    ├── 回复 Handshake{B_id, B_ts}
    │   ← 收到B的Handshake ─────────────┤
    │                                    │
    ├── A_id < B_id, A是获胜方           ├── B_id > A_id, B是落败方
    ├── 连接1是A主动dial → 保留          ├── 连接1是B被动accept → 关闭
    │                                    │
    │                    ┌─ 同时 ─┐      │
    │                    │        │      │
    │   (accept) ←── Dial ────────┤      │           连接2
    │   收到 Handshake{B_id, B_ts}│      ├── Send Handshake
    │   回复 Handshake{A_id, A_ts}│      │
    │                    │        │      │
    ├── A_id < B_id, A是获胜方    │      ├── B_id > A_id, B是落败方
    ├── 连接2是A被动accept → 关闭 │      ├── 连接2是B主动dial → 保留
    │                    │        │      │
    │   ===============================================
    │   最终保留: 连接1 (A→B)
    │   最终关闭: 连接2 (B→A)
最终只有一条 A(ID小) → B(ID大) 的连接存活，资源不浪费。
---
与现有代码的衔接点
文件	位置	需要做的事
transport_utils.go:78-83	handleHandShake	实现完整的握手交换 + 裁决逻辑
transport_utils.go:56-75	handleEachConn	先握手再进入消息循环，否则断开
transport_utils.go:39-53	accessLoop	新连接暂不注册，等握手成功后再按 PeerID 注册
transport.go:62-83	ConnectTo	Dial 成功后立即发送 Handshake，等待对方回复再裁决
transport.go:18	peer map	key 从 addr string 改为 peerID string
protocol/message.go:15-22	SyncMessage	可选增加 Timestamp int64 字段


# TESTING
TestTransport
├── 构造 & 身份
│   ├── NewTransport_ShouldHaveUniqueID        // MyID 非空，两次调用 ID 不同
│   ├── NewTransport_ShouldReturnConfiguredPort // Port() 返回传入值
│   └── NewTransport_ShouldAssignPortAfterStart // port=0 时 Start() 后 Port() > 0
│
├── Start / Stop 生命周期
│   ├── Start_ShouldListenOnPort               // 启动后 localhost:port 可达
│   ├── Start_ShouldFailOnOccupiedPort         // 端口被占时返回 error
│   ├── Stop_ShouldCloseListener               // Stop 后 accept 循环退出
│   ├── Stop_ShouldDisconnectAllPeers          // Stop 后 PeerCount() == 0
│   ├── Stop_ShouldBeIdempotent                // 连续两次 Stop 不 panic
│   └── Stop_BeforeStart_ShouldNotPanic        // 未 Start 直接 Stop 安全
│
├── ConnectTo
│   ├── ConnectTo_ShouldRegisterPeer           // 成功后 PeerCount() == 1
│   ├── ConnectTo_ShouldFailOnInvalidAddr      // 无效地址返回 error
│   ├── ConnectTo_ShouldTimeoutOnUnreachable   // 不可达地址在 deadline 内返回 error
│   └── ConnectTo_DuplicateShouldHandle        // 连接已存在 peer: 幂等或拒绝
│
├── 握手（核心）
│   ├── Handshake_DialerSendsFirst             // 发起方先发 Handshake
│   ├── Handshake_AcceptorReplies              // 接收方回复 Handshake
│   ├── Handshake_BothGetPeerID                // 双方确认对方 PeerID
│   ├── Handshake_TieBreak_SmallerIDWins       // PeerID 较小者保留 dial 连接
│   ├── Handshake_TieBreak_Symmetric           // 互换角色，结果一致
│   ├── Handshake_DuplicateConn_KeepsOne       // 双向同时连接 → 仅一条存活
│   ├── Handshake_LoserSendsReject             // 落败方关闭前发 MsgHandShakeReject
│   ├── Handshake_Timeout_ClosesConnection     // 对端 5 秒不发 Handshake → 断开
│   └── Handshake_InvalidFirstMessage          // 首条消息非 Handshake → 断开
│
├── SendTo / Broadcast
│   ├── SendTo_KnownPeer_DeliversMessage       // 发到已知 peer，对方收到
│   ├── SendTo_UnknownPeer_ReturnsError        // 发到未知 PeerID → error
│   ├── Broadcast_NPeers_AllReceive            // N 个 peer 全部收到
│   ├── Broadcast_ZeroPeers_Succeeds           // 无 peer 时不报错
│   └── Broadcast_OneDisconnected_SkipsPeer    // 某 peer 已断，跳过不阻塞
│
├── OnMessage（回调）
│   ├── OnMessage_CalledOnReceive              // 收到消息时触发回调
│   ├── OnMessage_PeerIDMatchesSender          // 回调参数 peerID 正确
│   ├── OnMessage_AllTypesRouted               // MsgNotify/MsgPullRequest/... 都触发
│   └── OnMessage_NilHandler_DoesNotCrash      // 未注册回调时不 panic
│
├── Peers / PeerCount（内省）
│   ├── Peers_EmptyInitially                   // 初始为空 slice
│   ├── Peers_ContainsPeerAfterConnect         // 连接后包含 peerID
│   ├── Peers_RemovesPeerAfterDisconnect       // 断开后不再包含
│   └── PeerCount_MatchesConnections           // PeerCount 与连接数一致
│
└── 并发安全（-race 标志）
    ├── ConcurrentConnectTo_NoRace             // 多 goroutine 同时 ConnectTo
    ├── ConcurrentBroadcast_NoRace             // 广播期间有新连接加入
    ├── ConcurrentReadPeers_DuringConnect      // ConnectTo 期间并发读 Peers
    └── ConcurrentStop_DuringConnect           // Stop 与 ConnectTo 并发
