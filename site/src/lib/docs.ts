import { readFile } from "node:fs/promises";
import path from "node:path";
import { documentSections, type DocumentSection } from "@/lib/docs-shared";

export { documentSections };

export type SemantixDocument = {
  slug: string;
  fileName: string;
  title: string;
  description: string;
  language: "中文" | "English";
  section: DocumentSection;
  navDescription: string;
  published: string;
  lastUpdated: string;
  evidence: "E0" | "E1" | "E2" | "E3";
};

export const documents: readonly SemantixDocument[] = [
  {
    slug: "quickstart",
    fileName: "quickstart.md",
    title: "安装与首次运行",
    description: "从 Release 或源码安装 Semantix，并完成第一次提取、检索与注入。",
    language: "中文",
    section: "开始使用",
    navDescription: "安装、构建与 30 秒闭环",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E1",
  },
  {
    slug: "agent-integration",
    fileName: "agent-integration.md",
    title: "接入 Coding Agent",
    description: "为 Semantix Agent、Claude Code 或自定义 harness 接入检索、注入与会话沉淀。",
    language: "中文",
    section: "开始使用",
    navDescription: "Semantix Agent、Claude Code 与自定义 harness",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E1",
  },
  {
    slug: "configuration",
    fileName: "configuration.md",
    title: "配置与作用域",
    description: "理解配置优先级、切片库路径、作用域、检索、注入和成本参数。",
    language: "中文",
    section: "开始使用",
    navDescription: "TOML、环境变量与 CLI 覆盖",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E1",
  },
  {
    slug: "extract-search",
    fileName: "extract-search.md",
    title: "提取与检索",
    description: "把会话 JSONL 转成语义切片，并使用 BM25、向量或混合模式检索。",
    language: "中文",
    section: "核心工作流",
    navDescription: "从会话到可搜索切片",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E1",
  },
  {
    slug: "lookup-inject",
    fileName: "lookup-inject.md",
    title: "Lookup 与上下文注入",
    description: "区分面向工具调用的结构化 lookup 与面向提示词的预算化 inject。",
    language: "中文",
    section: "核心工作流",
    navDescription: "结构化命中与 L2 复用块",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E1",
  },
  {
    slug: "verify-observe",
    fileName: "verify-observe.md",
    title: "验证、用量与健康检查",
    description: "用 verify、usage、dashboard 和 doctor 判断复用是否有效且可运行。",
    language: "中文",
    section: "核心工作流",
    navDescription: "命中率、成本、仪表盘与诊断",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E1",
  },
  {
    slug: "cli-reference",
    fileName: "cli-reference.md",
    title: "CLI 命令索引",
    description: "按任务查找 Semantix CLI 命令、输出契约和退出码。",
    language: "中文",
    section: "核心工作流",
    navDescription: "命令分组、JSON 信封与退出码",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E1",
  },
  {
    slug: "slices-and-cache",
    fileName: "slices-and-cache.md",
    title: "语义切片与三级缓存",
    description: "理解切片库以及 L1 前缀缓存、L2 上下文注入和 L3 结果复用的边界。",
    language: "中文",
    section: "理解机制",
    navDescription: "L1、L2、L3 各自解决什么",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E0",
  },
  {
    slug: "retrieval-safety",
    fileName: "retrieval-safety.md",
    title: "检索、分区与复用安全",
    description: "解释混合检索、hit/grey/miss 分区、依赖指纹、judge 与 fail-open 原则。",
    language: "中文",
    section: "理解机制",
    navDescription: "相关不等于可安全复用",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E0",
  },
  {
    slug: "scheduling-evolution",
    fileName: "scheduling-evolution.md",
    title: "调度、预取与参数演化",
    description: "理解 round plan、保守预取、浪费反馈和有界参数演化。",
    language: "中文",
    section: "理解机制",
    navDescription: "如何学习，但不越过安全边界",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E0",
  },
  {
    slug: "gateway",
    fileName: "gateway.md",
    title: "部署 OpenAI 兼容 Gateway",
    description: "通过 Docker Compose 部署 Semantix Gateway，并观察非流式与流式命中。",
    language: "中文",
    section: "集成与运维",
    navDescription: "透明代理、上游与缓存观测",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E1",
  },
  {
    slug: "storage-maintenance",
    fileName: "storage-maintenance.md",
    title: "存储、维护与安全",
    description: "备份切片库、运行 GC、理解归档文件，并处理权限与敏感数据。",
    language: "中文",
    section: "集成与运维",
    navDescription: "备份、GC、归档与密钥边界",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E1",
  },
  {
    slug: "evidence-and-status",
    fileName: "evidence-and-status.md",
    title: "实现状态与证据边界",
    description: "区分已实现代码、仓库测试、合成回放和待完成的真实生产验证。",
    language: "中文",
    section: "集成与运维",
    navDescription: "如何阅读测试、报告与路线图",
    published: "2026-08-22",
    lastUpdated: "2026-08-22",
    evidence: "E1",
  },
  {
    slug: "profile",
    fileName: "overview.md",
    title: "Semantix 项目速览",
    description: "面向开发者的中文项目定位、术语与进度概览。",
    language: "中文",
    section: "项目背景",
    navDescription: "定位、模块与当前阶段",
    published: "2026-08-10",
    lastUpdated: "2026-08-22",
    evidence: "E0",
  },
  {
    slug: "guide",
    fileName: "deep-dive.md",
    title: "从零理解 Semantix",
    description: "从第一性原理出发的中文深度解读。",
    language: "中文",
    section: "项目背景",
    navDescription: "从第一性原理理解设计选择",
    published: "2026-08-10",
    lastUpdated: "2026-08-22",
    evidence: "E0",
  },
  {
    slug: "profile-en",
    fileName: "overview.en.md",
    title: "Semantix Project Overview",
    description: "A concise English overview of the project's purpose, terminology, and progress.",
    language: "English",
    section: "项目背景",
    navDescription: "English overview",
    published: "2026-08-10",
    lastUpdated: "2026-08-22",
    evidence: "E0",
  },
  {
    slug: "guide-en",
    fileName: "deep-dive.en.md",
    title: "Understanding Semantix from Scratch",
    description: "An English deep dive from first principles.",
    language: "English",
    section: "项目背景",
    navDescription: "English deep dive",
    published: "2026-08-10",
    lastUpdated: "2026-08-22",
    evidence: "E0",
  },
];

export function getDocument(slug: string) {
  return documents.find((document) => document.slug === slug);
}

export async function readDocument(document: SemantixDocument) {
  const filePath = path.join(process.cwd(), "content", "docs", document.fileName);
  return readFile(filePath, "utf8");
}
