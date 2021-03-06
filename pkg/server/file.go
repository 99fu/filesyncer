package server

import (
	"io"
	"sync"
	"time"

	"github.com/fagongzi/log"
	"github.com/fagongzi/util/uuid"
	"github.com/infinivision/filesyncer/pkg/pb"
)

type fileManager struct {
	sync.RWMutex

	cfg   RetryCfg
	allc  uint64
	files map[uint64]*file
}

func newFileManager(cfg RetryCfg) *fileManager {
	return &fileManager{
		files: make(map[uint64]*file, 1024),
		cfg:   cfg,
	}
}

func (mgr *fileManager) addFile(req *pb.InitUploadReq) uint64 {
	mgr.Lock()
	fid := mgr.allc
	mgr.files[fid] = newFile(fid, req)
	mgr.allc++
	mgr.Unlock()

	log.Infof("file-%d: added with %d bytes and %d chunks",
		fid,
		req.ContentLength,
		req.ChunkCount)
	return fid
}

func (mgr *fileManager) appendFile(req *pb.UploadReq) pb.Code {
	mgr.Lock()

	if f, ok := mgr.files[req.ID]; ok {
		f.last = req.Index
		mgr.Unlock()
		return f.append(req)
	}

	mgr.Unlock()
	log.Debugf("file-%d: missing", req.ID)
	return pb.CodeMissing
}

func (mgr *fileManager) continueUpload(id uint64) (bool, int32) {
	mgr.RLock()

	if f, ok := mgr.files[id]; ok {
		idx := f.last
		mgr.RUnlock()
		return true, idx
	}

	mgr.RUnlock()
	return false, 0
}

func (mgr *fileManager) completeFile(req *pb.UploadCompleteReq) pb.Code {
	fid := req.ID

	mgr.RLock()
	if f, ok := mgr.files[fid]; ok {
		times := 0
		duration := mgr.cfg.RetryInterval

		for {
			if times > 0 {
				log.Infof("file-%d: retry the %d times",
					fid,
					times)
			}

			code := f.complete(req)
			if code != pb.CodeOSSError {
				mgr.remove(fid)
				mgr.RUnlock()
				return code
			}

			if times > 0 {
				duration = time.Duration(mgr.cfg.RetryFactor) * duration
			}

			times++
			if times >= mgr.cfg.MaxTimes {
				log.Warnf("file-%d: retry failed in %d times",
					fid,
					times)
				mgr.remove(fid)
				mgr.RUnlock()
				return pb.CodeMaxRetries
			}

			time.Sleep(duration)
		}
	}

	mgr.RUnlock()
	return pb.CodeMissing
}

func (mgr *fileManager) remove(id uint64) {
	delete(mgr.files, id)
	log.Infof("file-%d: removed", id)
}

type file struct {
	id     uint64
	meta   *pb.InitUploadReq
	chunks [][]byte
	readed int
	last   int32
}

func newFile(id uint64, meta *pb.InitUploadReq) *file {
	return &file{
		id:     id,
		meta:   meta,
		chunks: make([][]byte, meta.ChunkCount, meta.ChunkCount),
		last:   -1,
	}
}

func (f *file) append(req *pb.UploadReq) pb.Code {
	if req.Index >= f.meta.ChunkCount {
		log.Errorf("file-%d: append with invalid chunk idx %d",
			req.ID,
			req.Index)
		return pb.CodeInvalidChunk
	}

	if data := f.chunks[req.Index]; len(data) > 0 {
		log.Errorf("file-%d: already append with chunk idx %d",
			req.ID,
			req.Index)
		return pb.CodeSucc
	}

	f.chunks[req.Index] = req.Data
	log.Infof("file-%d: append %d bytes with chunk idx %d",
		req.ID,
		len(req.Data),
		req.Index)
	return pb.CodeSucc
}

func (f *file) complete(req *pb.UploadCompleteReq) pb.Code {
	objID := uuid.NewID()
	f.readed = 0
	err := objectStore.PutObject(bucketName, objID, f, f.meta.ContentLength)
	if err != nil {
		log.Errorf("file-%d: complete with oss errors: %+v",
			req.ID,
			err)
		return pb.CodeOSSError
	}

	log.Infof("file-%d: complete succ with object %s",
		req.ID,
		objID)
	return pb.CodeSucc
}

func (f *file) Read(p []byte) (int, error) {
	size := len(p)
	pos := 0
	read := 0
	for _, data := range f.chunks {
		cs := len(data)
		pos += cs
		if f.readed < pos {
			unreadIdx := cs - (pos - f.readed)
			n := copy(p[read:], data[unreadIdx:])
			read += n
			f.readed += n
			if read == size {
				return read, nil
			}
		}
	}

	return read, io.EOF
}
