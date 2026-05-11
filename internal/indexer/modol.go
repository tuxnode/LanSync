package indexer

type FileInfo struct {
	RelPath  string `json:"rel_path"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"mod_time"`
	Hash     string `json:"hash"`
	IsFolder bool   `json:"is_folder"`
}

type IndexMap map[string]FileInfo
