package service

// strPtrVal 解引用字符串指针（nil 视为空串），供去重 / 上传 / 文件服务共用
func strPtrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
