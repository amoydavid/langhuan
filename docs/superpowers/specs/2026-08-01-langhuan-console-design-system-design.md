# 琅嬛 Web Console Design System Skill 设计

**日期：** 2026-08-01

**状态：** 已确认

**目标目录：** `.codex/skills/langhuan-console-design-system/`

## 1. 目标

创建项目内技能 `langhuan-console-design-system`，用于规划、设计、实现和评审整个琅嬛 Web Console。技能必须适配当前 React 19、Tailwind CSS v4、shadcn/ui、Radix、TanStack Router/Query、React Hook Form、Zod 与 Lucide 实现，不引入第二套前端技术栈或脱离现有代码的组件模板。

技能覆盖以下工作：

- AppShell、导航、工作台、表格、表单、详情面板、状态反馈和空状态。
- 主题、排版、色彩、间距、密度、图标与品牌资产。
- URL 工作状态、权限表达、异步事实、可读名称和诊断信息边界。
- 桌面、平板、移动端响应式设计。
- 键盘、焦点、触控目标、减少动效和语义化无障碍。
- 前端实现前检查、测试和完成验收。

## 2. 事实源与优先级

技能采用“混合型、以现有代码为事实源”的组织方式。它保存稳定的设计合同和执行流程，但不复制当前组件实现。

发生冲突时按以下优先级处理：

1. 用户当前请求和仓库根目录 `AGENTS.md`。
2. 已批准的当前功能规格与真实后端/API 合同。
3. `web/src/styles/theme.css`、`web/src/styles/index.css`、`web/src/components/ui/`、`web/src/components/layout/` 和现有品牌资产所表达的当前实现。
4. 技能 references 中的稳定规则与示例。

技能不得把历史截图、GetRank HTML 模板或复制出来的 CSS 当成高于琅嬛当前代码的事实源。

## 3. 目录结构

```text
.codex/skills/langhuan-console-design-system/
├── SKILL.md
├── agents/
│   └── openai.yaml
└── references/
    ├── foundations.md
    ├── application-patterns.md
    ├── product-experience.md
    └── accessibility-and-verification.md
```

不创建 README、变更日志、独立组件代码、主题 CSS 副本、字体、图标或截图资产。当前项目代码就是可复用实现，避免技能与产品形成两套来源。

## 4. SKILL.md 职责

`SKILL.md` 保持精简，包含：

- 清晰的触发描述：任何琅嬛 Web Console 页面、组件、导航、表格、表单、状态、响应式、无障碍、视觉评审或前端体验规格工作。
- 开工前读取顺序：`AGENTS.md`、相关产品规格、当前主题和将要复用的组件。
- 设计与实现流程：确认事实和权限、确定路由状态、选择现有组件、适配响应式与无障碍、补测试并验证。
- 硬规则摘要：真实数据、现有技术栈、组件复用、完整 AppShell、可读名称、主题兼容、Lucide、触控与键盘语义。
- references 路由：只在任务需要时读取对应参考文件。

## 5. References 职责

### 5.1 foundations.md

记录已落地的视觉基础：

- 工程绿主题和语义颜色的使用原则。
- light、dark、system 三种主题合同；侧栏始终维持深色语义。
- Geist Sans、Inter、JetBrains Mono 的职责分工。
- 紧凑的数据工作台密度、圆角、边框、阴影和动效原则。
- Lucide React 单一图标来源与全局 `stroke-width: 1.5`。
- 琅嬛 Logo、favicon 与可访问名称的现有位置和使用边界。

具体 token 值不在 reference 中复制，要求实现者读取 `web/src/styles/theme.css`。

### 5.2 application-patterns.md

记录可复用页面与组件组合：

- Authenticated AppShell、深色侧栏、Header、Breadcrumb、主内容区和移动端导航抽屉。
- 页面标题、任务主动作、资源卡片、数据表格、移动端卡片、表单、状态标签、Sheet、Dialog、Toast、Skeleton 和空状态。
- TanStack Router/Query、RHF + Zod 与 mutation invalidation 的现有边界。
- 先查找现有 `components/ui` 和跨 feature 组件，再决定是否新增封装。
- UI primitive 尽量不直接改；业务定制优先通过 feature 组件或包装组件完成。

### 5.3 product-experience.md

记录跨版本稳定的产品体验规则：

- 围绕用户任务组织页面，不按数据库表或接口堆叠页面。
- URL 表达可恢复的工作状态，深链接、刷新、前进后退必须保持语义。
- 权限沿用后端合同；隐藏写操作不代替服务端授权。
- 只显示真实 API 状态、统计和进度；不得生成装饰性 KPI、阶段或成功率。
- UUID、hash 和内部 lineage 用于寻址与诊断，不在普通界面裸露；资源使用可读名称。
- 异步 mutation 立即显示真实 pending/building 状态，后台完成更新页面，失败说明影响与下一步。
- Markdown、错误、安全字段和 Workspace 隔离继续服从当前功能规格与 API 合同。

### 5.4 accessibility-and-verification.md

记录响应式、无障碍和完成检查：

- 桌面使用适合二维数据的表格；移动端使用任务等价的卡片或单栏页面，不缩小宽表格。
- 视觉控件保持约 36px 的紧凑密度；移动端通过外层布局、padding 或伪元素扩展交互命中区，保证至少 `44×44px`。
- 状态不只依赖颜色；需要文字，并在必要时配合图标或 tone。
- 维护 skip link、键盘导航、焦点进入与恢复、字段错误关联和 `prefers-reduced-motion`。
- 验证 light/dark/system、关键断点、无横向页面溢出、直达/F5、权限状态和真实 API 数据。
- 完成前根据改动范围运行 `pnpm --dir web check`、`pnpm --dir web test`、`pnpm --dir web build` 与 `git diff --check`；数据库 E2E 继续使用临时 pgvector/Redis。

## 6. 关键设计决策

### 6.1 保留已经落地的工程绿系统

当前 Web Console 已实际使用工程绿 token、深色侧栏、浅色或深色内容画布、三套字体和紧凑表格。新技能固化这些事实，不把它描述为待实施的重品牌工作。

### 6.2 同时保留紧凑密度与触控可达性

桌面按钮、输入框和图标按钮继续保持当前约 36px 的视觉高度。移动端不得机械把所有组件放大为笨重的 44px 外观，而应保证可点击区域、行高、间距或透明命中层达到至少 `44×44px`。任何视觉密度规则都不能覆盖键盘和触控可访问性。

### 6.3 支持完整主题合同

技能必须支持当前 light、dark、system 三种主题，不采用“只允许浅色画布”的旧限制。新增样式只使用语义 token，并在两种实际渲染主题下检查对比度、边框、焦点和状态。

### 6.4 不复制 GetRank 产物

GetRank 设计技能可解释这套视觉语言的来源，但其产品 IA、文案、HTML/CSS 模板、KPI 和图表形态不是琅嬛事实。琅嬛技能只保留已经进入当前代码且符合琅嬛任务模型的原则。

### 6.5 不重复实现产品规范

技能提供跨版本稳定的工作方法，不复制完整的 v0.5.0 体验规格。具体工作台路由、权限矩阵、状态枚举和 API 字段仍从当前批准规格和实现读取，避免技能在后续版本过期。

## 7. 成功标准

技能完成后应满足：

1. 名称和目录符合 Codex skill 规范，`SKILL.md` frontmatter 只有 `name` 与 `description`。
2. `agents/openai.yaml` 的展示名称、简述和默认提示与技能内容一致。
3. 另一位 Agent 仅凭该技能即可先定位真实主题和组件，再设计或实现符合现有系统的页面。
4. 技能不会引导 Agent 复制 GetRank HTML/CSS、发明假数据、绕过 AppShell、裸露内部 ID 或破坏明暗主题。
5. 技能明确解决 36px 视觉密度与 44px 移动端命中区的关系。
6. `quick_validate.py` 校验通过，文件内没有未完成占位标记、占位说明或无效路径。

## 8. 验证方案

- 运行 skill-creator 的 `quick_validate.py`。
- 检查 SKILL.md 与 references 不重复大段内容，所有 reference 都能从 SKILL.md 一层到达。
- 检查引用的仓库路径在当前 checkout 中存在。
- 使用一个典型任务前向验证，例如“设计并实现知识库检索结果页”，确认输出会复用 AppShell、主题 token、TanStack Query、真实状态、移动端卡片和无障碍规则。
- 检查 `.gitignore` 和其它用户已有改动未被覆盖。
