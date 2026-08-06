package feishu

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/dajee/langhuan/internal/domain/model"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// ParseURL 把用户输入的飞书分享链接或裸 token 解析为 SyncRoot。
//
// 识别形式：
//   - https://<host>.feishu.cn/wiki/<node_token>           → wiki_node
//   - https://<host>.feishu.cn/drive/folder/<folder_token>  → drive_folder
//   - https://<host>.feishu.cn/docx/<document_id>           → wiki_node（单文档，按 wiki 根处理）
//   - 裸 token（无法判别类型）                                → wiki_node（首版默认）
//
// 首版 wiki 同步根只接受 wiki node；docx URL 当作单文档 wiki 根交给 ListTree 处理。
func ParseURL(input string) (model.SyncRoot, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return model.SyncRoot{}, fmt.Errorf("输入不能为空")
	}

	// 形如 https://xxx.feishu.cn/<kind>/<token>[?query][#fragment]
	if strings.HasPrefix(strings.ToLower(raw), "http://") || strings.HasPrefix(strings.ToLower(raw), "https://") {
		return parseFeishuURL(raw)
	}

	// 裸 token：无法判别类型，首版按 wiki_node 处理。
	return model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: raw}, nil
}

func parseFeishuURL(raw string) (model.SyncRoot, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return model.SyncRoot{}, fmt.Errorf("无法解析飞书 URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if !isFeishuHost(host) {
		return model.SyncRoot{}, fmt.Errorf("非飞书域名: %s", parsed.Host)
	}

	// 用 Path 拆段，忽略 query/fragment。
	path := strings.Trim(parsed.Path, "/")
	segments := strings.Split(path, "/")
	if len(segments) == 0 {
		return model.SyncRoot{}, fmt.Errorf("URL 缺少路径段: %s", raw)
	}

	kind := segments[0]
	switch kind {
	case "wiki":
		if len(segments) < 2 || segments[1] == "" {
			return model.SyncRoot{}, fmt.Errorf("wiki URL 缺少 node_token")
		}
		return model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: segments[1]}, nil
	case "docx":
		if len(segments) < 2 || segments[1] == "" {
			return model.SyncRoot{}, fmt.Errorf("docx URL 缺少 document_id")
		}
		// 首版：单文档按 wiki 同步根处理（ListTree 会经 get_node 解析）。
		return model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: segments[1]}, nil
	case "drive":
		// /drive/folder/<token> 或 /folder/<token>
		if len(segments) >= 3 && segments[1] == "folder" && segments[2] != "" {
			return model.SyncRoot{Kind: sourceport.SyncRootDriveFolder, Token: segments[2]}, nil
		}
		if len(segments) >= 2 && segments[1] == "folder" {
			return model.SyncRoot{}, fmt.Errorf("drive folder URL 缺少 folder_token")
		}
		return model.SyncRoot{}, fmt.Errorf("drive URL 暂不支持: %s", raw)
	default:
		return model.SyncRoot{}, fmt.Errorf("无法识别的飞书 URL 类型 %q", kind)
	}
}

// isFeishuHost 判定 host 是否属于飞书/lark 域名族（含 *.feishu.cn / *.larksuite.com）。
func isFeishuHost(host string) bool {
	if host == "" {
		return false
	}
	return strings.HasSuffix(host, ".feishu.cn") ||
		strings.HasSuffix(host, ".larksuite.com") ||
		host == "feishu.cn" ||
		host == "larksuite.com"
}
