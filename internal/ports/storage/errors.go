package storage

import "errors"

// ErrObjectNotFound 是跨 adapter 的「对象不存在」sentinel error。
//
// local / S3 adapter 的 Open / Delete 在底层（os.IsNotExist / S3 NoSuchKey）返回错误时，
// 必须用 fmt.Errorf("...: %w", ErrObjectNotFound) 包装它，使调用方能够通过 errors.Is
// 统一判断对象已不存在，从而在幂等清理场景安全跳过。
var ErrObjectNotFound = errors.New("storage object not found")
