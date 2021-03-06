package server

import (
	"github.com/fagongzi/log"
	"github.com/infinivision/filesyncer/pkg/oss"
)

var (
	fileMgr     *fileManager
	objectStore oss.ObjectStorage
	bucketName  string
)

func initG(cfg *Cfg) {
	bucketName = cfg.Oss.BucketName
	initFileManager(cfg.Retry)
	initObjectStore(cfg.Oss)
}

func initFileManager(cfg RetryCfg) {
	fileMgr = newFileManager(cfg)
}

func initObjectStore(cfg OssCfg) {
	var err error
	objectStore, err = oss.NewMinioStorage(cfg.Server, cfg.Key, cfg.SecretKey, cfg.UseSSL)
	if err != nil {
		log.Fatalf("init oss store failed with %+v, errors: %+v", cfg, err)
	}
}
