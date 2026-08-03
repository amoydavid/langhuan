package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// FileTreeNodeType identifies a virtual file-tree node.
type FileTreeNodeType string

const (
	FileTreeNodeRoot   FileTreeNodeType = "root"
	FileTreeNodeFolder FileTreeNodeType = "folder"
	FileTreeNodeFile   FileTreeNodeType = "file"
)

// Validate rejects unknown file-tree node types.
func (t FileTreeNodeType) Validate() error {
	switch t {
	case FileTreeNodeRoot, FileTreeNodeFolder, FileTreeNodeFile:
		return nil
	default:
		return fmt.Errorf("%w: 无效的文件树节点类型 %q", domainerrors.ErrValidation, t)
	}
}
