# redis-tui 功能实现计划

对标 Medis 2(docs.getmedis.com,17 页文档全部核对)。单二进制 Go 程序:tview 界面骨架(紧凑风格原型已验证)+ go-redis v9 协议层。

- 优先级:P0 = MVP 必须,P1 = 完整对标,P2 = 加分项
- 规模:S ≤ 2 天,M ≤ 4 天,L ≥ 1 周
- 每条含:功能点(→ Medis 对应文档)、实现要点、验收标准

## 包结构

```
cmd/redis-tui/       入口、CLI flags
internal/
  app/       tview 应用壳:布局、焦点、Modal、状态栏、键位路由
  theme/     色板 tokens、dark/light、NO_COLOR 降级
  conn/      profile 管理、直连/TLS/SSH 隧道/cluster、连接测试
  scanner/   SCAN 引擎、前缀树、键元数据 pipeline
  keyview/   各类型查看器/编辑器、键操作
  cmdquery/  词法分析、高亮渲染、执行器、历史、补全
  cmdtable/  COMMAND 表加载、读写分类
  encode/    格式探测、内置编解码、外部 encoder 协议、内容规则
  config/    TOML 读写(~/.config/redis-tui/config.toml)
  i18n/      zh-CN / en-US
```

## 阶段 0 — 骨架(P0,S)

- [ ] 0.1 项目布局、go.mod、Makefile(build/lint/test)、golangci-lint 通过
- [ ] 0.2 tview 应用壳:三栏布局(迁移 /tmp/medis-tui 原型)、Tab 焦点循环、Modal 框架、1 行命令栏 + 1 行状态栏
  - 验收:窗口 80×24~200×60 缩放不破版,Ctrl+C 干净退出
- [ ] 0.3 主题系统:色板 token 化(原型已定:类型 6 色、选中 #314259、边框 #3b4256)、dark/light、NO_COLOR=1 时自动降级单色
  - → Medis「Dark mode」
- [ ] 0.4 文件日志(TUI 占用 tty):~/.local/share/redis-tui/log,级别可配
- [ ] 0.5 全局键位路由:j/k/h/l、面板切换、? 帮助、q 退出、a 切 alert

## 阶段 1 — 连接层(P0,M)

- [ ] 1.1 Profile 模型 + TOML 持久化:host/port/ACL username/password/default db/TLS/cluster/SSH/内容规则/免删确认
- [ ] 1.2 直连:go-redis Options + ACL;SELECT 默认库
  - → Medis「Default Database」
- [ ] 1.3 连接测试与信息:PING、INFO server/memory;版本、内存、延迟进状态栏
- [ ] 1.4 TLS:crypto/tls,可配 insecure(ElastiCache/Upstash/DigitalOcean/MemoryDB 场景,四篇文档均为「填 host/port/password + 开 SSL」)
  - 验收:Upstash/DO 带 TLS 的真实实例连通
- [ ] 1.5 SSH 隧道:x/crypto/ssh 本地 listener → 注入 go-redis Options.Dialer;支持密码/私钥/agent;解析 ~/.ssh/config(kevinburke/ssh/config)
  - → Medis「SSH tunnel」「Custom SSH Config」「1Password SSH agent」(agent 走 SSH_AUTH_SOCK,天然支持)
- [ ] 1.6 Cluster:ClusterClient,节点自动发现、按 slot 分发;节点/槽位状态展示;命令查询默认随机节点(与 Medis 一致,其文档明确此 caveat)
  - → Medis「Cluster」「ElastiCache(cluster via SSH tunnel)」「MemoryDB」
- [ ] 1.7 连接管理 UI:profile 列表/新建/编辑/复制/删除/测试,表单 Modal + 密码不明文回显
- [ ] 1.8 CLI 入口:`redis://[user:pass@]host:port/db` URL、--tls、--ssh、-c profile
  - → Medis「Custom URL Scheme」(medis:// 的 TUI 等价;注册系统 URL scheme 列为 P2)
  - 验收:文档示例 medis://username:password@localhost:6379/2 的每个字段均可用等价 CLI/URL 表达

## 阶段 2 — 键浏览(P0,L)

- [ ] 2.1 SCAN 引擎:后台 goroutine 游标分页(COUNT 可配)、MATCH、切换过滤即取消旧扫描、增量批量回调;绝不 KEYS
  - → Medis「High performance:百万键不阻塞」
  - 验收:灌 1,000,000 键,滚动与过滤全程 UI 不卡,扫描可中断
- [ ] 2.2 键元数据:TYPE/PTTL/MEMORY USAGE 批量 pipeline,可见行优先惰性补全
- [ ] 2.3 前缀树:`:` 分隔增量构建、分隔符可配、max fold level 设置、懒展开(展开 = SCAN prefix:*)
  - → Medis「Key Browser:tree view + fold level」
- [ ] 2.4 键列表:列(key/type/ttl/size)、模式过滤、类型过滤、TTL 临期红色、排序、虚拟滚动
- [ ] 2.5 详情查看器(全部类型):
  - string:GET + 格式化查看;编辑保存 SET(保持 TTL)
  - hash:HSCAN 分页 field/value 表;HSET / HDEL / 字段重命名(HSET+HDEL)
  - list:LRANGE 分页(带索引列);LSET/LPUSH/RPUSH/LREM/LPOP/RPOP
  - set:SSCAN 成员表;SADD/SREM
  - zset:ZSCAN(member+score);ZADD/ZREM/ZINCRBY、score 内联编辑
  - stream:XRANGE 分页(id+fields)、XLEN、XADD;consumer group 视图(XINFO GROUPS/CONSUMERS)P2
  - RedisJSON:MODULE LIST 探测;type=ReJSON-RL → JSON.GET 美化、JSON.SET 保存、JSONPath 查询
  - → Medis「Support all key types」「RedisJSON」
  - 验收:每类型写入→查看→修改→回读真库,值与 TTL 一致
- [ ] 2.6 大 value 与二进制:虚拟滚动 + 截断加载(>1MB 分段);非法 UTF-8 自动 hex 视图(text/hex 双栏)
- [ ] 2.7 键操作:DEL(确认框,per-connection 可关)、RENAME、COPY、EXPIRE/PERSIST(TTL 内联编辑)、多选批量删
  - → Medis「Delete Confirmation Dialog」
- [ ] 2.8 联动:树选命名空间 → 列表过滤;列表选键 → 详情异步加载(加载中骨架行)

## 阶段 3 — 命令查询(P0 核心体验,P1 全量,L)

- [ ] 3.1 词法分析器(文法完全对齐 Medis command-query 文档):
  - `"…"` / `'…'` / 裸词;`\"` `\\` 转义;双引号内 `\n` 展开为换行;引号跨行 = 多行参数;换行分隔多命令(引号内除外);命令名大小写不敏感
  - 验收:文档全部示例(multiline eval、转义、\n)解析结果逐 token 正确;表驱动单测直接引用文档用例
- [ ] 3.2 命令表:连接时执行 COMMAND 拉取 name/arity/flags(readonly/write);离线 fallback 内置副本
  - 用途:高亮分类、alert 判定、补全 —— 不硬编码命令清单,随服务端版本走
- [ ] 3.3 输入组件:多行编辑 + 逐 token 高亮(只读=橙 / 写=蓝 / 字符串 / 数字),当前行浅灰底(Execute Selected 预览);tview TextArea 上叠自绘渲染层
  - → Medis「Command colors」「Execute Selected」
- [ ] 3.4 执行:⏎ 执行光标行;选区多行批量执行;结果追加到输出视图并保持滚动
- [ ] 3.5 Alert mode:write 命令 → 确认 Modal(完整命令 + 目标 key + 影响预览);全局 a 切换,默认关
  - → Medis「Alert mode」
- [ ] 3.6 结果渲染:status/int/bulk/array(表格化)/nil/error(红);自动格式探测(阶段 4 联动);大结果分页
- [ ] 3.7 历史(↑↓ 检索 + 持久化)与补全:命令名 + 键名(SCAN 缓存种子)

## 阶段 4 — 编码器与内容规则(P1,M)

- [ ] 4.1 内置:JSON(美化+校验)、MessagePack(vmihailenco/msgpack v5)、Gzip(compress/gzip)、PHP serialize、hex、纯文本;自动探测 = try-decode 链 + 启发式
  - → Medis「Support JSON/MessagePack」+ 内置 encoders(MessagePack/Gzip/PHP)
- [ ] 4.2 自定义 encoder:与 Medis 完全同协议 —— `~/.config/redis-tui/encoders/encoder_*` 可执行文件,shebang 决定解释器,Base64 走 stdin/stdout,argv[1]=decode|encode,非零退出码 = 错误
  - 验收:Medis 文档的 encoder_Reverse.sh 示例原样可跑(decode 反转、encode 报错路径都有提示)
- [ ] 4.3 Content Rules:per-connection 规则表(key glob `*`/`?` + type 双条件 AND → viewer/encoder,留空=全匹配,"Auto"=默认行为);对 hash/list/set/zset 的字段生效
  - → Medis「Content Rules」
- [ ] 4.4 查看器:text / JSON(语法着色)/ hex+ASCII 双栏;msgpack 树形视图 P2

## 阶段 5 — 体验与发布(P1,M)

- [ ] 5.1 in-app 设置 Modal(分隔符、fold level、SCAN COUNT、主题、语言、alert 默认)+ config.toml 双向同步
- [ ] 5.2 i18n:zh-CN/en-US,默认随 $LANG
  - → Medis「Language Settings」
- [ ] 5.3 帮助:?(全屏键位表)、底栏常驻 hints(lazygit 式,原型已实现)
- [ ] 5.4 服务器页:INFO 解析(内存/clients/ops/命中率/keyspace)、db 0-15 快速切换(SELECT)
- [ ] 5.5 交互打磨:面板宽度鼠标拖拽、OSC52 剪贴板复制(SSH 场景可用)、鼠标滚动
- [ ] 5.6 发布:goreleaser(多平台)、Homebrew tap、README + GIF

## P2(明确降级/暂缓)

- medis:// 式系统 URL scheme 注册(redis-tui://)
- stream consumer group 深度管理、msgpack 树视图
- Sentinel 支持(Medis 文档未强调)
- 键的 DUMP/RESTORE、批量导入导出

## 横切 — 测试与质量

- lexer / 格式探测 / content rules / 命令分类:表驱动单测(用例直接取自 Medis 文档示例)
- 协议交互:testcontainers-go 起 redis + redisjson 镜像,全类型读写集成测试
- 压测:脚本灌 100 万键冒烟(慢速测试,本地跑)
- UI:不做组件单测;tmux 脚本化冒烟(启动→按键→capture-pane 断言,管线已验证)

## 依赖

rivo/tview · gdamore/tcell/v2 · redis/go-redis/v9 · golang.org/x/crypto/ssh · kevinburke/ssh/config · vmihailenco/msgpack/v5 · BurntSushi/toml · testcontainers-go(dev)

## 里程碑

- M1(≈3 周):阶段 0-2 → 能连、能浏览、能看全类型 —— 可日常用
- M2(≈5 周):+ 阶段 3 → 命令行查询 + alert —— 对标核心体验完成
- M3(≈7 周):+ 阶段 4-5 → 完整对标 Medis 功能面
