// langhuan-eval 是琅嬛的离线检索评测 harness（spec：docs/superpowers/specs/
// 2026-08-24-retrieval-eval-design.md）。它是一个独立命令，不进入琅嬛主链路：
//
//	langhuan-eval prepare   下载 MIRACL-zh 并确定性采样双轨评测数据集
//	langhuan-eval run       拉起被测系统、执行通道矩阵检索并产出指标报告
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "langhuan-eval:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		usage()
		return nil
	}
	switch args[1] {
	case "prepare":
		return runPrepareCommand(args[2:])
	case "run":
		return runRunCommand(args[2:])
	case "mock-embedding":
		return runMockEmbedding(args[2:])
	case "version":
		fmt.Println("langhuan-eval (与琅嬛仓库同版本发布，见报告指纹 repo_head)")
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("未知子命令 %q（可用：prepare / run / version）", args[1])
	}
}

func runPrepareCommand(args []string) error {
	fs := flag.NewFlagSet("prepare", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dataset := fs.String("dataset", "miracl-zh", "数据集：miracl-zh（维基百科段落/长文）或 vcsum（中文会议转写，无结构连续文本）")
	dataDir := fs.String("data-dir", ".eval-data", "数据集根目录（相对仓库根）")
	cacheDir := fs.String("cache-dir", ".eval-data/cache", "原始文件共享缓存目录（相对仓库根；不同数据集共用，避免重复下载）")
	mirror := fs.String("mirror", "https://hf-mirror.com", "HuggingFace 镜像端点")
	fallback := fs.String("fallback", "https://huggingface.co", "镜像失败后的直连端点")
	queries := fs.Int("queries", 200, "采样 query 数")
	distractors := fs.Int("distractors", 4800, "Track A 干扰段落数（gold 之外语料规模）")
	distractorArticles := fs.Int("distractor-articles", 300, "Track B 干扰文章数")
	maxPassages := fs.Int("max-passages", 40, "Track B 单篇文章段落截断上限")
	seed := fs.Int64("seed", 20260824, "确定性采样 seed")
	vcsumSource := fs.String("vcsum-source", vcsumSourceBase, "vcsum 源文件根端点（GitHub raw）")
	vcsumQueryMeetings := fs.Int("vcsum-query-meetings", vcsumQueryMeetings, "vcsum 取前 N 场对齐会议的话题段构造 query")
	vcsumVariant := fs.String("vcsum-variant", vcsumVariantPlain, "vcsum 语料变体：空=原文 / heading=注入人工话题标题 / heading-neutral=注入中性标题（oracle 实验）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	target := *dataDir
	if !filepath.IsAbs(target) {
		target = filepath.Join(repoRoot, target)
	}
	cache := *cacheDir
	if !filepath.IsAbs(cache) {
		cache = filepath.Join(repoRoot, cache)
	}
	switch *dataset {
	case "miracl-zh", "": // 缺省兼容历史用法
		return prepareMIRACLChinese(prepareOptions{
			DataDir: target, CacheDir: cache,
			Mirror: *mirror, Fallback: *fallback,
			Queries: *queries, Distractors: *distractors,
			DistractorArticles: *distractorArticles, MaxPassagesPerArticle: *maxPassages, Seed: *seed,
		})
	case "vcsum":
		return prepareVCSUM(vcsumPrepareOptions{
			DataDir: target, CacheDir: cache,
			SourceBaseURL: *vcsumSource, QueryMeetings: *vcsumQueryMeetings,
			Variant: *vcsumVariant,
		})
	default:
		return fmt.Errorf("未知数据集 %q（可用：miracl-zh / vcsum）", *dataset)
	}
}

func runRunCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "eval.config.yaml", "评测配置文件（不存在时用内置默认值）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runEval(*configPath)
}

func usage() {
	fmt.Print(`langhuan-eval —— 琅嬛离线检索评测

用法：
  langhuan-eval prepare [flags]         准备数据集（默认 MIRACL-zh，双轨）
  langhuan-eval run    [flags]          执行评测（默认 standalone 拉起被测系统）
  langhuan-eval mock-embedding [flags]  本地确定性 mock embedding（离线冒烟用）

常用示例：
  go run ./cmd/langhuan-eval prepare            # 首次约下载 730MB 语料（走镜像）
  go run ./cmd/langhuan-eval prepare -dataset vcsum   # 会议转写语料（约 30MB）
  go run ./cmd/langhuan-eval run                # 使用 eval.config.yaml（或默认值）
  make eval                                     # 等价于上面两步

完整设计见 docs/superpowers/specs/2026-08-24-retrieval-eval-design.md。
`)
}
