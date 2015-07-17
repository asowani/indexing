// Copyright (c) 2014 Couchbase, Inc.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
// except in compliance with the License. You may obtain a copy of the License at
//   http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the
// License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
// either express or implied. See the License for the specific language governing permissions
// and limitations under the License.

package indexer

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"github.com/couchbase/indexing/secondary/common"
	"github.com/couchbase/indexing/secondary/fdb"
	"github.com/couchbase/indexing/secondary/logging"
	projClient "github.com/couchbase/indexing/secondary/projector/client"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Indexer interface {
	Shutdown() Message
}

var StreamAddrMap StreamAddressMap
var StreamTopicName map[common.StreamId]string

type BucketIndexCountMap map[string]int
type BucketFlushInProgressMap map[string]bool
type BucketObserveFlushDoneMap map[string]MsgChannel
type BucketRequestStopCh map[string]StopChannel
type BucketRollbackTs map[string]*common.TsVbuuid

// Errors
var (
	ErrFatalComm                = errors.New("Fatal Internal Communication Error")
	ErrInconsistentState        = errors.New("Inconsistent Internal State")
	ErrKVRollbackForInitRequest = errors.New("KV Rollback Received For Initial Build Request")
	ErrMaintStreamMissingBucket = errors.New("Bucket Missing in Maint Stream")
	ErrInvalidStream            = errors.New("Invalid Stream")
	ErrIndexerInRecovery        = errors.New("Indexer In Recovery")
	ErrKVConnect                = errors.New("Error Connecting KV")
	ErrUnknownBucket            = errors.New("Unknown Bucket")
)

type indexer struct {
	id string

	indexInstMap  common.IndexInstMap //map of indexInstId to IndexInst
	indexPartnMap IndexPartnMap       //map of indexInstId to PartitionInst

	streamBucketStatus map[common.StreamId]BucketStatus

	streamBucketFlushInProgress  map[common.StreamId]BucketFlushInProgressMap
	streamBucketObserveFlushDone map[common.StreamId]BucketObserveFlushDoneMap

	streamBucketRequestStopCh map[common.StreamId]BucketRequestStopCh
	streamBucketRollbackTs    map[common.StreamId]BucketRollbackTs
	streamBucketRequestQueue  map[common.StreamId]map[string]chan *kvRequest
	streamBucketRequestLock   map[common.StreamId]map[string]chan *sync.Mutex

	bucketBuildTs map[string]Timestamp

	//TODO Remove this once cbq bridge support goes away
	bucketCreateClientChMap map[string]MsgChannel

	wrkrRecvCh          MsgChannel //channel to receive messages from workers
	internalRecvCh      MsgChannel //buffered channel to queue worker requests
	adminRecvCh         MsgChannel //channel to receive admin messages
	internalAdminRecvCh MsgChannel //internal channel to receive admin messages
	internalAdminRespCh chan bool  //internal channel to respond admin messages
	shutdownInitCh      MsgChannel //internal shutdown channel for indexer
	shutdownCompleteCh  MsgChannel //indicate shutdown completion

	mutMgrCmdCh        MsgChannel //channel to send commands to mutation manager
	storageMgrCmdCh    MsgChannel //channel to send commands to storage manager
	tkCmdCh            MsgChannel //channel to send commands to timekeeper
	adminMgrCmdCh      MsgChannel //channel to send commands to admin port manager
	compactMgrCmdCh    MsgChannel //channel to send commands to compaction manager
	clustMgrAgentCmdCh MsgChannel //channel to send messages to index coordinator
	kvSenderCmdCh      MsgChannel //channel to send messages to kv sender
	cbqBridgeCmdCh     MsgChannel //channel to send message to cbq sender
	settingsMgrCmdCh   MsgChannel
	statsMgrCmdCh      MsgChannel
	scanCoordCmdCh     MsgChannel //chhannel to send messages to scan coordinator

	mutMgrExitCh MsgChannel //channel to indicate mutation manager exited

	tk            Timekeeper        //handle to timekeeper
	storageMgr    StorageManager    //handle to storage manager
	compactMgr    CompactionManager //handle to compaction manager
	mutMgr        MutationManager   //handle to mutation manager
	adminMgr      AdminManager      //handle to admin port manager
	clustMgrAgent ClustMgrAgent     //handle to ClustMgrAgent
	kvSender      KVSender          //handle to KVSender
	cbqBridge     CbqBridge         //handle to CbqBridge
	settingsMgr   settingsManager
	statsMgr      statsManager
	scanCoord     ScanCoordinator //handle to ScanCoordinator
	config        common.Config

	kvlock sync.Mutex //fine-grain lock for KVSender

	stats *IndexerStats

	enableManager bool
	cpuProfFd     *os.File
}

type kvRequest struct {
	lock     *sync.Mutex
	bucket   string
	streamId common.StreamId
	grantCh  chan bool
}

func NewIndexer(config common.Config) (Indexer, Message) {

	idx := &indexer{
		wrkrRecvCh:          make(MsgChannel),
		internalRecvCh:      make(MsgChannel, WORKER_MSG_QUEUE_LEN),
		adminRecvCh:         make(MsgChannel, WORKER_MSG_QUEUE_LEN),
		internalAdminRecvCh: make(MsgChannel),
		internalAdminRespCh: make(chan bool),
		shutdownInitCh:      make(MsgChannel),
		shutdownCompleteCh:  make(MsgChannel),

		mutMgrCmdCh:        make(MsgChannel),
		storageMgrCmdCh:    make(MsgChannel),
		tkCmdCh:            make(MsgChannel),
		adminMgrCmdCh:      make(MsgChannel),
		compactMgrCmdCh:    make(MsgChannel),
		clustMgrAgentCmdCh: make(MsgChannel),
		kvSenderCmdCh:      make(MsgChannel),
		cbqBridgeCmdCh:     make(MsgChannel),
		settingsMgrCmdCh:   make(MsgChannel),
		statsMgrCmdCh:      make(MsgChannel),
		scanCoordCmdCh:     make(MsgChannel),

		mutMgrExitCh: make(MsgChannel),

		indexInstMap:  make(common.IndexInstMap),
		indexPartnMap: make(IndexPartnMap),

		streamBucketStatus:           make(map[common.StreamId]BucketStatus),
		streamBucketFlushInProgress:  make(map[common.StreamId]BucketFlushInProgressMap),
		streamBucketObserveFlushDone: make(map[common.StreamId]BucketObserveFlushDoneMap),
		streamBucketRequestStopCh:    make(map[common.StreamId]BucketRequestStopCh),
		streamBucketRollbackTs:       make(map[common.StreamId]BucketRollbackTs),
		streamBucketRequestQueue:     make(map[common.StreamId]map[string]chan *kvRequest),
		streamBucketRequestLock:      make(map[common.StreamId]map[string]chan *sync.Mutex),
		bucketBuildTs:                make(map[string]Timestamp),
		bucketCreateClientChMap:      make(map[string]MsgChannel),
	}

	logging.Infof("Indexer::NewIndexer Status INIT")
	snapshotNotifych := make(chan IndexSnapshot, 100)

	var res Message
	idx.settingsMgr, idx.config, res = NewSettingsManager(idx.settingsMgrCmdCh, idx.wrkrRecvCh, config)
	if res.GetMsgType() != MSG_SUCCESS {
		logging.Errorf("Indexer::NewIndexer settingsMgr Init Error", res)
		return nil, res
	}

	idx.stats = NewIndexerStats()

	// Read memquota setting
	idx.stats.memoryQuota.Set(int64(idx.config["settings.memory_quota"].Uint64()))
	logging.Infof("Indexer::NewIndexer Starting with Vbuckets %v", idx.config["numVbuckets"].Int())

	idx.initStreamAddressMap()
	idx.initStreamFlushMap()

	//Start Mutation Manager
	idx.mutMgr, res = NewMutationManager(idx.mutMgrCmdCh, idx.wrkrRecvCh, idx.config)
	if res.GetMsgType() != MSG_SUCCESS {
		logging.Errorf("Indexer::NewIndexer Mutation Manager Init Error", res)
		return nil, res
	}

	//Start KV Sender
	idx.kvSender, res = NewKVSender(idx.kvSenderCmdCh, idx.wrkrRecvCh, idx.config)
	if res.GetMsgType() != MSG_SUCCESS {
		logging.Errorf("Indexer::NewIndexer KVSender Init Error", res)
		return nil, res
	}

	//Start Timekeeper
	idx.tk, res = NewTimekeeper(idx.tkCmdCh, idx.wrkrRecvCh, idx.config)
	if res.GetMsgType() != MSG_SUCCESS {
		logging.Errorf("Indexer::NewIndexer Timekeeper Init Error", res)
		return nil, res
	}

	//Start Scan Coordinator
	idx.scanCoord, res = NewScanCoordinator(idx.scanCoordCmdCh, idx.wrkrRecvCh, idx.config, snapshotNotifych)
	if res.GetMsgType() != MSG_SUCCESS {
		logging.Errorf("Indexer::NewIndexer Scan Coordinator Init Error", res)
		return nil, res
	}

	idx.enableManager = idx.config["enableManager"].Bool()

	if idx.enableManager {
		idx.clustMgrAgent, res = NewClustMgrAgent(idx.clustMgrAgentCmdCh, idx.adminRecvCh, idx.config)
		if res.GetMsgType() != MSG_SUCCESS {
			logging.Errorf("Indexer::NewIndexer ClusterMgrAgent Init Error", res)
			return nil, res
		}
	}

	idx.statsMgr, res = NewStatsManager(idx.statsMgrCmdCh, idx.wrkrRecvCh, idx.config)
	if res.GetMsgType() != MSG_SUCCESS {
		logging.Errorf("Indexer::NewIndexer statsMgr Init Error", res)
		return nil, res
	}

	//read persisted indexer state
	if err := idx.bootstrap(snapshotNotifych); err != nil {
		logging.Fatalf("Indexer::Unable to Bootstrap Indexer from Persisted Metadata.")
		return nil, &MsgError{err: Error{cause: err}}
	}

	if !idx.enableManager {
		//Start CbqBridge
		idx.cbqBridge, res = NewCbqBridge(idx.cbqBridgeCmdCh, idx.adminRecvCh, idx.indexInstMap, idx.config)
		if res.GetMsgType() != MSG_SUCCESS {
			logging.Errorf("Indexer::NewIndexer CbqBridge Init Error", res)
			return nil, res
		}
	}

	//Register with Index Coordinator
	if err := idx.registerWithCoordinator(); err != nil {
		//log error and exit
	}

	//sync topology
	if err := idx.syncTopologyWithCoordinator(); err != nil {
		//log error and exit
	}

	//Start Admin port listener
	idx.adminMgr, res = NewAdminManager(idx.adminMgrCmdCh, idx.adminRecvCh)
	if res.GetMsgType() != MSG_SUCCESS {
		logging.Errorf("Indexer::NewIndexer Admin Manager Init Error", res)
		return nil, res
	}

	logging.Infof("Indexer::NewIndexer Status ACTIVE")

	idx.compactMgr, res = NewCompactionManager(idx.compactMgrCmdCh, idx.wrkrRecvCh, idx.config)
	if res.GetMsgType() != MSG_SUCCESS {
		logging.Errorf("Indexer::NewCompactionmanager Init Error", res)
		return nil, res
	}

	// Setup http server
	addr := net.JoinHostPort("", idx.config["httpPort"].String())
	logging.PeriodicProfile(logging.Debug, addr, "goroutine")
	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			logging.Errorf("indexer:: Error Starting Http Server: %v", err)
			common.CrashOnError(err)
		}
	}()

	//start the main indexer loop
	idx.run()

	return idx, &MsgSuccess{}

}

func (idx *indexer) acquireStreamRequestLock(bucket string, streamId common.StreamId) *kvRequest {

	// queue request
	request := &kvRequest{grantCh: make(chan bool, 1), lock: nil, bucket: bucket, streamId: streamId}

	idx.kvlock.Lock()
	defer idx.kvlock.Unlock()

	// allocate the request queue
	rq, ok := idx.streamBucketRequestQueue[streamId][bucket]
	if !ok {
		rq = make(chan *kvRequest, 5000)

		if _, ok = idx.streamBucketRequestQueue[streamId]; !ok {
			idx.streamBucketRequestQueue[streamId] = make(map[string]chan *kvRequest)
		}
		idx.streamBucketRequestQueue[streamId][bucket] = rq
	}

	// allocate the lock
	lq, ok := idx.streamBucketRequestLock[streamId][bucket]
	if !ok {
		lq = make(chan *sync.Mutex, 1) // hold one lock

		if _, ok = idx.streamBucketRequestLock[streamId]; !ok {
			idx.streamBucketRequestLock[streamId] = make(map[string]chan *sync.Mutex)
		}
		idx.streamBucketRequestLock[streamId][bucket] = lq

		// seed the lock
		lq <- new(sync.Mutex)
	}

	// acquire the lock if it is available and there is no other request ahead of me
	if len(rq) == 0 && len(lq) == 1 {
		request.lock = <-lq
		request.grantCh <- true
	} else if len(rq) < 5000 {
		rq <- request
	} else {
		common.CrashOnError(errors.New("acquireStreamRequestLock: too many requests acquiring stream request lock"))
	}

	return request
}

func (idx *indexer) waitStreamRequestLock(req *kvRequest) {

	<-req.grantCh
}

func (idx *indexer) releaseStreamRequestLock(req *kvRequest) {

	if req.lock == nil {
		return
	}

	idx.kvlock.Lock()
	defer idx.kvlock.Unlock()

	streamId := req.streamId
	bucket := req.bucket

	rq, ok := idx.streamBucketRequestQueue[streamId][bucket]
	if ok && len(rq) != 0 {
		next := <-rq
		next.lock = req.lock
		next.grantCh <- true
	} else {
		if lq, ok := idx.streamBucketRequestLock[streamId][bucket]; ok {
			lq <- req.lock
		} else {
			common.CrashOnError(errors.New("releaseStreamRequestLock: streamBucketRequestLock is not initialized"))
		}
	}
}

func (idx *indexer) registerWithCoordinator() error {

	//get the IndexerId from persistence and send it to Index Coordinator

	//if there is no IndexerId, send an empty one. Coordinator will assign
	//a new IndexerId in that case and treat this as a fresh node.
	return nil

}

func (idx *indexer) syncTopologyWithCoordinator() error {

	//get the latest topology from coordinator
	return nil
}

func (idx *indexer) recoverPersistedSnapshots() error {

	//recover persisted snapshots from disk
	return nil

}

//run starts the main loop for the indexer
func (idx *indexer) run() {

	go idx.listenWorkerMsgs()
	go idx.listenAdminMsgs()

	for {

		select {

		case msg, ok := <-idx.internalRecvCh:
			if ok {
				idx.handleWorkerMsgs(msg)
			}

		case msg, ok := <-idx.internalAdminRecvCh:
			if ok {
				idx.handleAdminMsgs(msg)
				idx.internalAdminRespCh <- true
			}

		case <-idx.shutdownInitCh:
			//send shutdown to all workers

			idx.shutdownWorkers()
			//close the shutdown complete channel to indicate
			//all workers are shutdown
			close(idx.shutdownCompleteCh)
			return

		}

	}

}

//run starts the main loop for the indexer
func (idx *indexer) listenAdminMsgs() {

	waitForStream := true

	for {
		select {
		case msg, ok := <-idx.adminRecvCh:
			if ok {
				// internalAdminRecvCh size is 1.   So it will blocked if the previous msg is being
				// processed.
				idx.internalAdminRecvCh <- msg
				<-idx.internalAdminRespCh

				if waitForStream {
					// now that indexer has processed the message.  Let's make sure that
					// the stream request is finished before processing the next admin
					// msg.  This is done by acquiring a lock on the stream request for each
					// bucket (on both streams).   The lock is FIFO, so if this function
					// can get a lock, it will mean that previous stream request would have
					// been cleared.
					buckets := idx.getBucketForAdminMsg(msg)
					for _, bucket := range buckets {
						f := func(streamId common.StreamId, bucket string) {
							lock := idx.acquireStreamRequestLock(bucket, streamId)
							defer idx.releaseStreamRequestLock(lock)
							idx.waitStreamRequestLock(lock)
						}

						f(common.INIT_STREAM, bucket)
						f(common.MAINT_STREAM, bucket)
					}
				}
			}
		case <-idx.shutdownInitCh:
			return
		}
	}
}

func (idx *indexer) getBucketForAdminMsg(msg Message) []string {
	switch msg.GetMsgType() {

	case CLUST_MGR_CREATE_INDEX_DDL, CBQ_CREATE_INDEX_DDL:
		createMsg := msg.(*MsgCreateIndex)
		return []string{createMsg.GetIndexInst().Defn.Bucket}

	case CLUST_MGR_BUILD_INDEX_DDL:
		buildMsg := msg.(*MsgBuildIndex)
		return buildMsg.GetBucketList()

	case CLUST_MGR_DROP_INDEX_DDL, CBQ_DROP_INDEX_DDL:
		dropMsg := msg.(*MsgDropIndex)
		return []string{dropMsg.GetBucket()}

	default:
		return nil
	}
}

func (idx *indexer) listenWorkerMsgs() {

	//listen to worker messages
	for {

		select {

		case msg, ok := <-idx.wrkrRecvCh:
			if ok {
				//handle high priority messages
				switch msg.GetMsgType() {
				case MSG_ERROR:
					err := msg.(*MsgError).GetError()
					if err.code == ERROR_MUT_MGR_PANIC {
						close(idx.mutMgrExitCh)
					}
				}
				idx.internalRecvCh <- msg
			}

		case <-idx.shutdownInitCh:
			//exit the loop
			return
		}
	}

}

func (idx *indexer) handleWorkerMsgs(msg Message) {

	switch msg.GetMsgType() {

	case STREAM_READER_HWT:
		//fwd the message to timekeeper
		idx.tkCmdCh <- msg
		<-idx.tkCmdCh

	case STREAM_READER_STREAM_BEGIN:

		//fwd the message to timekeeper
		idx.tkCmdCh <- msg
		<-idx.tkCmdCh

	case STREAM_READER_STREAM_END:

		//fwd the message to timekeeper
		idx.tkCmdCh <- msg
		<-idx.tkCmdCh

	case STREAM_READER_STREAM_DROP_DATA:
		//TODO
		logging.Debugf("Indexer::handleWorkerMsgs Received Drop Data "+
			"From Mutation Mgr %v", msg)

	case STREAM_READER_SNAPSHOT_MARKER:
		//fwd the message to timekeeper
		idx.tkCmdCh <- msg
		<-idx.tkCmdCh

	case STREAM_READER_CONN_ERROR:

		//fwd the message to timekeeper
		idx.tkCmdCh <- msg
		<-idx.tkCmdCh

	case TK_STABILITY_TIMESTAMP:
		//send TS to Mutation Manager
		ts := msg.(*MsgTKStabilityTS).GetTimestamp()
		bucket := msg.(*MsgTKStabilityTS).GetBucket()
		streamId := msg.(*MsgTKStabilityTS).GetStreamId()
		changeVec := msg.(*MsgTKStabilityTS).GetChangeVector()

		if idx.streamBucketStatus[streamId][bucket] == STREAM_INACTIVE {
			logging.Debugf("Indexer: Skipped PersistTs for %v %v. "+
				"STREAM_INACTIVE", streamId, bucket)
			return
		}

		idx.streamBucketFlushInProgress[streamId][bucket] = true

		idx.mutMgrCmdCh <- &MsgMutMgrFlushMutationQueue{
			mType:     MUT_MGR_PERSIST_MUTATION_QUEUE,
			bucket:    bucket,
			ts:        ts,
			streamId:  streamId,
			changeVec: changeVec}

		<-idx.mutMgrCmdCh

	case MUT_MGR_ABORT_PERSIST:

		idx.mutMgrCmdCh <- msg
		<-idx.mutMgrCmdCh

	case MUT_MGR_FLUSH_DONE:

		idx.storageMgrCmdCh <- msg
		<-idx.storageMgrCmdCh

	case MUT_MGR_ABORT_DONE:

		idx.tkCmdCh <- msg
		<-idx.tkCmdCh

	case STORAGE_SNAP_DONE:

		bucket := msg.(*MsgMutMgrFlushDone).GetBucket()
		streamId := msg.(*MsgMutMgrFlushDone).GetStreamId()

		idx.streamBucketFlushInProgress[streamId][bucket] = false

		//if there is any observer for flush done, notify
		idx.notifyFlushObserver(msg)

		//fwd the message to timekeeper
		idx.tkCmdCh <- msg
		<-idx.tkCmdCh

	case TK_INIT_BUILD_DONE:
		idx.handleInitialBuildDone(msg)

	case TK_MERGE_STREAM:
		idx.handleMergeStream(msg)

	case INDEXER_PREPARE_RECOVERY:
		idx.handlePrepareRecovery(msg)

	case INDEXER_INITIATE_RECOVERY:
		idx.handleInitRecovery(msg)

	case STORAGE_INDEX_SNAP_REQUEST,
		STORAGE_INDEX_STORAGE_STATS,
		STORAGE_INDEX_COMPACT:
		idx.storageMgrCmdCh <- msg
		<-idx.storageMgrCmdCh

	case CONFIG_SETTINGS_UPDATE:
		cfgUpdate := msg.(*MsgConfigUpdate)
		newConfig := cfgUpdate.GetConfig()
		if newConfig["settings.memory_quota"].Uint64() !=
			idx.config["settings.memory_quota"].Uint64() {
			idx.stats.needsRestart.Set(true)
		}
		idx.setProfilerOptions(newConfig)
		idx.config = newConfig
		idx.compactMgrCmdCh <- msg
		<-idx.compactMgrCmdCh
		idx.tkCmdCh <- msg
		<-idx.tkCmdCh
		idx.scanCoordCmdCh <- msg
		<-idx.scanCoordCmdCh
		idx.kvSenderCmdCh <- msg
		<-idx.kvSenderCmdCh
		idx.mutMgrCmdCh <- msg
		<-idx.mutMgrCmdCh
		idx.statsMgrCmdCh <- msg
		<-idx.statsMgrCmdCh
		idx.updateSliceWithConfig(newConfig)

	case INDEXER_INIT_PREP_RECOVERY:
		idx.handleInitPrepRecovery(msg)

	case INDEXER_PREPARE_DONE:
		idx.handlePrepareDone(msg)

	case INDEXER_RECOVERY_DONE:
		idx.handleRecoveryDone(msg)

	case KV_STREAM_REPAIR:
		idx.handleKVStreamRepair(msg)

	case TK_INIT_BUILD_DONE_ACK:
		idx.handleInitBuildDoneAck(msg)

	case TK_MERGE_STREAM_ACK:
		idx.handleMergeStreamAck(msg)

	case STREAM_REQUEST_DONE:
		idx.handleStreamRequestDone(msg)

	case KV_SENDER_RESTART_VBUCKETS:

		//fwd the message to kv_sender
		idx.sendMsgToKVSender(msg)

	case STORAGE_STATS:
		idx.storageMgrCmdCh <- msg
		<-idx.storageMgrCmdCh

	case SCAN_STATS:
		idx.scanCoordCmdCh <- msg
		<-idx.scanCoordCmdCh

	case INDEX_PROGRESS_STATS:
		idx.tkCmdCh <- msg
		<-idx.tkCmdCh

	case INDEXER_BUCKET_NOT_FOUND:
		idx.handleBucketNotFound(msg)

	case INDEXER_STATS:
		idx.handleStats(msg)

	case MSG_ERROR:
		//crash for all errors by default
		logging.Fatalf("Indexer::handleWorkerMsgs Fatal Error On Worker Channel %+v", msg)
		err := msg.(*MsgError).GetError()
		common.CrashOnError(err.cause)

	case STATS_RESET:
		idx.handleResetStats()

	default:
		logging.Errorf("Indexer::handleWorkerMsgs Unknown Message %+v", msg)
		common.CrashOnError(errors.New("Unknown Msg On Worker Channel"))
	}

}

func (idx *indexer) handleAdminMsgs(msg Message) {

	switch msg.GetMsgType() {

	case CLUST_MGR_CREATE_INDEX_DDL,
		CBQ_CREATE_INDEX_DDL:

		idx.handleCreateIndex(msg)

	case CLUST_MGR_BUILD_INDEX_DDL:
		idx.handleBuildIndex(msg)

	case CLUST_MGR_DROP_INDEX_DDL,
		CBQ_DROP_INDEX_DDL:

		idx.handleDropIndex(msg)

	case MSG_ERROR:

		logging.Fatalf("Indexer::handleAdminMsgs Fatal Error On Admin Channel %+v", msg)
		err := msg.(*MsgError).GetError()
		common.CrashOnError(err.cause)

	default:
		logging.Errorf("Indexer::handleAdminMsgs Unknown Message %+v", msg)
		common.CrashOnError(errors.New("Unknown Msg On Admin Channel"))

	}

}

//TODO handle panic, otherwise main loop will get shutdown
func (idx *indexer) handleCreateIndex(msg Message) {

	indexInst := msg.(*MsgCreateIndex).GetIndexInst()
	clientCh := msg.(*MsgCreateIndex).GetResponseChannel()

	logging.Infof("Indexer::handleCreateIndex %v", indexInst)

	if !ValidateBucket(idx.config["clusterAddr"].String(), indexInst.Defn.Bucket, []string{indexInst.Defn.BucketUUID}) {
		logging.Errorf("Indexer::handleCreateIndex \n\t Bucket %v Not Found")

		if clientCh != nil {
			clientCh <- &MsgError{
				err: Error{code: ERROR_INDEXER_UNKNOWN_BUCKET,
					severity: FATAL,
					cause:    ErrUnknownBucket,
					category: INDEXER}}

		}
		return
	}

	if idx.streamBucketStatus[common.INIT_STREAM][indexInst.Defn.Bucket] == STREAM_RECOVERY ||
		idx.streamBucketStatus[common.INIT_STREAM][indexInst.Defn.Bucket] == STREAM_PREPARE_RECOVERY ||
		idx.streamBucketStatus[common.MAINT_STREAM][indexInst.Defn.Bucket] == STREAM_RECOVERY ||
		idx.streamBucketStatus[common.MAINT_STREAM][indexInst.Defn.Bucket] == STREAM_PREPARE_RECOVERY {
		logging.Errorf("Indexer::handleCreateIndex \n\tCannot Process Create Index " +
			"In Recovery Mode.")

		if clientCh != nil {
			clientCh <- &MsgError{
				err: Error{code: ERROR_INDEXER_IN_RECOVERY,
					severity: FATAL,
					cause:    ErrIndexerInRecovery,
					category: INDEXER}}

		}
		return
	}

	//check if this is duplicate index instance
	if ok := idx.checkDuplicateIndex(indexInst, clientCh); !ok {
		return
	}

	idx.stats.AddIndex(indexInst.InstId, indexInst.Defn.Bucket, indexInst.Defn.Name)
	//allocate partition/slice
	var partnInstMap PartitionInstMap
	var err error
	if partnInstMap, err = idx.initPartnInstance(indexInst, clientCh); err != nil {
		return
	}

	//update index maps with this index
	idx.indexInstMap[indexInst.InstId] = indexInst
	idx.indexPartnMap[indexInst.InstId] = partnInstMap

	msgUpdateIndexInstMap := idx.newIndexInstMsg(idx.indexInstMap)
	msgUpdateIndexPartnMap := &MsgUpdatePartnMap{indexPartnMap: idx.indexPartnMap}

	if err := idx.distributeIndexMapsToWorkers(msgUpdateIndexInstMap, msgUpdateIndexPartnMap); err != nil {
		if clientCh != nil {
			clientCh <- &MsgError{
				err: Error{code: ERROR_INDEXER_INTERNAL_ERROR,
					severity: FATAL,
					cause:    err,
					category: INDEXER}}
		}
		common.CrashOnError(err)
	}

	if idx.enableManager {
		clientCh <- &MsgSuccess{}
	} else {
		//for cbq bridge, simulate build index
		idx.handleBuildIndex(&MsgBuildIndex{indexInstList: []common.IndexInstId{indexInst.InstId},
			respCh: clientCh})
	}

}

func (idx *indexer) handleBuildIndex(msg Message) {

	instIdList := msg.(*MsgBuildIndex).GetIndexList()
	clientCh := msg.(*MsgBuildIndex).GetRespCh()

	logging.Infof("Indexer::handleBuildIndex %v", instIdList)

	if len(instIdList) == 0 {
		logging.Warnf("Indexer::handleBuildIndex Nothing To Build")
		if clientCh != nil {
			clientCh <- &MsgSuccess{}
		}
	}

	bucketIndexList := idx.groupIndexListByBucket(instIdList)
	errMap := make(map[common.IndexInstId]error)

	initialBuildReqd := true
	for bucket, instIdList := range bucketIndexList {

		if ok := idx.checkValidIndexInst(bucket, instIdList, clientCh, errMap); !ok {
			logging.Errorf("Indexer::handleBuildIndex \n\tInvalid Index List "+
				"Bucket %v. IndexList %v", bucket, instIdList)
			if idx.enableManager {
				delete(bucketIndexList, bucket)
				continue
			} else {
				return
			}
		}

		if !idx.checkBucketExists(bucket, instIdList, clientCh, errMap) {
			logging.Errorf("Indexer::handleBuildIndex \n\tCannot Process Build Index."+
				"Unknown Bucket %v.", bucket)
			if idx.enableManager {
				delete(bucketIndexList, bucket)
				continue
			} else {
				return
			}
		}

		if ok := idx.checkBucketInRecovery(bucket, instIdList, clientCh, errMap); ok {
			logging.Errorf("Indexer::handleBuildIndex \n\tCannot Process Build Index "+
				"In Recovery Mode. Bucket %v. IndexList %v", bucket, instIdList)
			if idx.enableManager {
				delete(bucketIndexList, bucket)
				continue
			} else {
				return
			}
		}

		//check if Initial Build is already running for this index's bucket
		if ok := idx.checkDuplicateInitialBuildRequest(bucket, instIdList, clientCh, errMap); !ok {
			logging.Errorf("Indexer::handleBuildIndex \n\tBuild Already In"+
				"Progress. Bucket %v.", bucket)
			if idx.enableManager {
				delete(bucketIndexList, bucket)
				continue
			} else {
				return
			}
		}

		cluster := idx.config["clusterAddr"].String()
		numVbuckets := idx.config["numVbuckets"].Int()
		buildTs, err := GetCurrentKVTs(cluster, "default", bucket, numVbuckets)
		if err != nil {
			errStr := fmt.Sprintf("Error Connecting KV %v Err %v",
				idx.config["clusterAddr"].String(), err)
			logging.Errorf("Indexer::handleBuildIndex %v", errStr)
			if idx.enableManager {
				idx.bulkUpdateError(instIdList, errStr)
				for _, instId := range instIdList {
					errMap[instId] = errors.New(errStr)
				}
				delete(bucketIndexList, bucket)
				continue
			} else if clientCh != nil {
				clientCh <- &MsgError{
					err: Error{code: ERROR_INDEXER_IN_RECOVERY,
						severity: FATAL,
						cause:    errors.New(errStr),
						category: INDEXER}}
				return
			}
		}

		//if there is already an index for this bucket in MAINT_STREAM,
		//add this index to INIT_STREAM
		var buildStream common.StreamId
		if idx.checkBucketExistsInStream(bucket, common.MAINT_STREAM, false) {
			buildStream = common.INIT_STREAM
		} else {
			buildStream = common.MAINT_STREAM
		}

		idx.bulkUpdateStream(instIdList, buildStream)

		//if initial build TS is zero and index belongs to MAINT_STREAM
		//initial build is not required.
		var buildState common.IndexState
		if buildTs.IsZeroTs() && buildStream == common.MAINT_STREAM {
			initialBuildReqd = false
		}
		//always set state to Initial, once stream request/build is done,
		//this will get changed to active
		buildState = common.INDEX_STATE_INITIAL

		idx.bulkUpdateState(instIdList, buildState)

		logging.Debugf("Indexer::handleBuildIndex \n\tAdded Index: %v to Stream: %v State: %v",
			instIdList, buildStream, buildState)

		msgUpdateIndexInstMap := idx.newIndexInstMsg(idx.indexInstMap)

		if err := idx.distributeIndexMapsToWorkers(msgUpdateIndexInstMap, nil); err != nil {
			if clientCh != nil {
				clientCh <- &MsgError{
					err: Error{code: ERROR_INDEXER_INTERNAL_ERROR,
						severity: FATAL,
						cause:    err,
						category: INDEXER}}
			}
			common.CrashOnError(err)
		}

		//send Stream Update to workers
		idx.sendStreamUpdateForBuildIndex(instIdList, buildStream, bucket, buildTs, clientCh)

		if _, ok := idx.streamBucketStatus[buildStream]; !ok {
			idx.streamBucketStatus[buildStream] = make(BucketStatus)
		}
		idx.streamBucketStatus[buildStream][bucket] = STREAM_ACTIVE

		//store updated state and streamId in meta store
		if idx.enableManager {
			if err := idx.updateMetaInfoForIndexList(instIdList, true,
				true, false, true); err != nil {
				common.CrashOnError(err)
			}
		} else {

			//if initial build is not being done, send success response,
			//otherwise success response will be sent when initial build gets done
			if !initialBuildReqd {
				clientCh <- &MsgSuccess{}
			} else {
				idx.bucketCreateClientChMap[bucket] = clientCh
			}
			return
		}
	}

	if idx.enableManager {
		clientCh <- &MsgBuildIndexResponse{errMap: errMap}
	} else {
		clientCh <- &MsgSuccess{}
	}

}

//TODO handle panic, otherwise main loop will get shutdown
func (idx *indexer) handleDropIndex(msg Message) {

	indexInstId := msg.(*MsgDropIndex).GetIndexInstId()
	clientCh := msg.(*MsgDropIndex).GetResponseChannel()

	logging.Infof("Indexer::handleDropIndex - IndexInstId %v", indexInstId)

	var indexInst common.IndexInst
	var ok bool
	if indexInst, ok = idx.indexInstMap[indexInstId]; !ok {

		errStr := fmt.Sprintf("Unknown Index Instance %v", indexInstId)
		logging.Errorf("Indexer::handleDropIndex %v", errStr)

		if clientCh != nil {
			clientCh <- &MsgError{
				err: Error{code: ERROR_INDEXER_UNKNOWN_INDEX,
					severity: FATAL,
					cause:    errors.New(errStr),
					category: INDEXER}}
		}
		return
	}

	idx.stats.RemoveIndex(indexInst.InstId)
	//if the index state is Created/Ready/Deleted, only data cleanup is
	//required. No stream updates are required.
	if indexInst.State == common.INDEX_STATE_CREATED ||
		indexInst.State == common.INDEX_STATE_READY ||
		indexInst.State == common.INDEX_STATE_DELETED {

		idx.cleanupIndexData(indexInst, clientCh)
		logging.Debugf("Indexer::handleDropIndex Cleanup Successful for "+
			"Index Data %v", indexInst)
		clientCh <- &MsgSuccess{}
		return
	}

	//Drop is a two step process. First set the index state as DELETED.
	//Then all the workers are notified about this state change. If this
	//step is successful, no mutation/scan request for the index will be processed.
	//Second step, is the actual cleanup of index instance from internal maps
	//and purging of physical slice files.

	indexInst.State = common.INDEX_STATE_DELETED
	idx.indexInstMap[indexInst.InstId] = indexInst

	msgUpdateIndexInstMap := idx.newIndexInstMsg(idx.indexInstMap)

	if err := idx.distributeIndexMapsToWorkers(msgUpdateIndexInstMap, nil); err != nil {
		clientCh <- &MsgError{
			err: Error{code: ERROR_INDEXER_INTERNAL_ERROR,
				severity: FATAL,
				cause:    err,
				category: INDEXER}}
		common.CrashOnError(err)
	}

	//if this is the last index for the bucket in MaintStream and the bucket exists
	//in InitStream, don't cleanup bucket from stream. It is needed for merge to
	//happen.
	if indexInst.Stream == common.MAINT_STREAM &&
		!idx.checkBucketExistsInStream(indexInst.Defn.Bucket, common.MAINT_STREAM, false) &&
		idx.checkBucketExistsInStream(indexInst.Defn.Bucket, common.INIT_STREAM, false) {
		logging.Warnf("Indexer::handleDropIndex Pre-Catchup Index Found for %v "+
			"%v. Stream Cleanup Skipped.", indexInst.Stream, indexInst.Defn.Bucket)
		clientCh <- &MsgSuccess{}
		return
	}

	if idx.streamBucketStatus[common.MAINT_STREAM][indexInst.Defn.Bucket] == STREAM_RECOVERY ||
		idx.streamBucketStatus[common.MAINT_STREAM][indexInst.Defn.Bucket] == STREAM_PREPARE_RECOVERY ||
		idx.streamBucketStatus[common.INIT_STREAM][indexInst.Defn.Bucket] == STREAM_RECOVERY ||
		idx.streamBucketStatus[common.INIT_STREAM][indexInst.Defn.Bucket] == STREAM_PREPARE_RECOVERY {

		logging.Errorf("Indexer::handleDropIndex Cannot Process Drop Index " +
			"In Recovery Mode.")

		if clientCh != nil {
			clientCh <- &MsgError{
				err: Error{code: ERROR_INDEXER_IN_RECOVERY,
					severity: FATAL,
					cause:    ErrIndexerInRecovery,
					category: INDEXER}}

		}
		return
	}

	//check if there is already a drop request waiting on this bucket
	if ok := idx.checkDuplicateDropRequest(indexInst, clientCh); ok {
		return
	}

	//if there is a flush in progress for this index's bucket and stream
	//wait for the flush to finish before drop
	streamId := indexInst.Stream
	bucket := indexInst.Defn.Bucket

	if ok, _ := idx.streamBucketFlushInProgress[streamId][bucket]; ok {
		notifyCh := make(MsgChannel)
		idx.streamBucketObserveFlushDone[streamId][bucket] = notifyCh
		go idx.processDropAfterFlushDone(indexInst, notifyCh, clientCh)
	} else {
		idx.cleanupIndex(indexInst, clientCh)
	}

}

func (idx *indexer) handlePrepareRecovery(msg Message) {

	streamId := msg.(*MsgRecovery).GetStreamId()
	bucket := msg.(*MsgRecovery).GetBucket()

	logging.Debugf("Indexer::handlePrepareRecovery StreamId %v Bucket %v",
		streamId, bucket)

	idx.stopBucketStream(streamId, bucket)

}

func (idx *indexer) handleInitPrepRecovery(msg Message) {

	bucket := msg.(*MsgRecovery).GetBucket()
	streamId := msg.(*MsgRecovery).GetStreamId()
	rollbackTs := msg.(*MsgRecovery).GetRestartTs()

	if rollbackTs != nil {
		if _, ok := idx.streamBucketRollbackTs[streamId]; ok {
			idx.streamBucketRollbackTs[streamId][bucket] = rollbackTs
		} else {
			bucketRollbackTs := make(BucketRollbackTs)
			bucketRollbackTs[bucket] = rollbackTs
			idx.streamBucketRollbackTs[streamId] = bucketRollbackTs
		}
	}

	idx.streamBucketStatus[streamId][bucket] = STREAM_PREPARE_RECOVERY

	logging.Debugf("Indexer::handleInitPrepRecovery StreamId %v Bucket %v %v",
		streamId, bucket, idx.streamBucketStatus[streamId][bucket])

	//fwd the msg to timekeeper
	idx.tkCmdCh <- msg
	<-idx.tkCmdCh
}

func (idx *indexer) handlePrepareDone(msg Message) {

	bucket := msg.(*MsgRecovery).GetBucket()
	streamId := msg.(*MsgRecovery).GetStreamId()

	logging.Debugf("Indexer::handlePrepareDone StreamId %v Bucket %v",
		streamId, bucket)

	delete(idx.streamBucketRequestStopCh[streamId], bucket)

	//fwd the msg to timekeeper
	idx.tkCmdCh <- msg
	<-idx.tkCmdCh

}

func (idx *indexer) handleInitRecovery(msg Message) {

	streamId := msg.(*MsgRecovery).GetStreamId()
	bucket := msg.(*MsgRecovery).GetBucket()
	restartTs := msg.(*MsgRecovery).GetRestartTs()

	idx.streamBucketStatus[streamId][bucket] = STREAM_RECOVERY

	logging.Debugf("Indexer::handleInitRecovery StreamId %v Bucket %v %v",
		streamId, bucket, idx.streamBucketStatus[streamId][bucket])

	//if there is a rollbackTs, process rollback
	if ts, ok := idx.streamBucketRollbackTs[streamId][bucket]; ok && ts != nil {
		restartTs, err := idx.processRollback(streamId, bucket, ts)
		if err != nil {
			common.CrashOnError(err)
		}
		idx.startBucketStream(streamId, bucket, restartTs)
	} else {
		idx.startBucketStream(streamId, bucket, restartTs)
	}

}

func (idx *indexer) handleRecoveryDone(msg Message) {

	bucket := msg.(*MsgRecovery).GetBucket()
	streamId := msg.(*MsgRecovery).GetStreamId()
	buildTs := msg.(*MsgRecovery).GetBuildTs()

	logging.Debugf("Indexer::handleRecoveryDone StreamId %v Bucket %v ",
		streamId, bucket)

	//send the msg to timekeeper
	idx.tkCmdCh <- msg
	<-idx.tkCmdCh

	delete(idx.streamBucketRequestStopCh[streamId], bucket)
	delete(idx.streamBucketRollbackTs[streamId], bucket)

	idx.bucketBuildTs[bucket] = buildTs

	//during recovery, if all indexes of a bucket gets dropped,
	//the stream needs to be stopped for that bucket.
	if !idx.checkBucketExistsInStream(bucket, streamId, false) {
		logging.Debugf("Indexer::handleRecoveryDone StreamId %v Bucket %v. No Index Found."+
			"Cleaning up.", streamId, bucket)
		idx.stopBucketStream(streamId, bucket)
		idx.streamBucketStatus[streamId][bucket] = STREAM_INACTIVE
	} else {
		//change status to Active
		idx.streamBucketStatus[streamId][bucket] = STREAM_ACTIVE
	}

	logging.Debugf("Indexer::handleRecoveryDone StreamId %v Bucket %v %v",
		streamId, bucket, idx.streamBucketStatus[streamId][bucket])

}

func (idx *indexer) handleKVStreamRepair(msg Message) {

	bucket := msg.(*MsgKVStreamRepair).GetBucket()
	streamId := msg.(*MsgKVStreamRepair).GetStreamId()
	restartTs := msg.(*MsgKVStreamRepair).GetRestartTs()

	//repair is not required for inactive bucket streams
	if idx.streamBucketStatus[streamId][bucket] == STREAM_INACTIVE {
		logging.Debugf("Indexer::handleKVStreamRepair Skip Stream Repair %v Inactive Bucket %v",
			streamId, bucket)
		return
	}

	//if there is already a repair in progress for this bucket stream
	//ignore the request
	if idx.checkStreamRequestPending(streamId, bucket) == false {
		logging.Debugf("Indexer::handleKVStreamRepair Initiate Stream Repair %v Bucket %v",
			streamId, bucket)
		idx.startBucketStream(streamId, bucket, restartTs)
	} else {
		logging.Debugf("Indexer::handleKVStreamRepair Ignore Stream Repair Request for Stream "+
			"%v Bucket %v. Request In Progress.", streamId, bucket)
	}

}

func (idx *indexer) handleInitBuildDoneAck(msg Message) {

	streamId := msg.(*MsgTKInitBuildDone).GetStreamId()
	bucket := msg.(*MsgTKInitBuildDone).GetBucket()

	//skip processing initial build done ack for inactive or recovery streams.
	//the streams would be restarted and then build done would get recomputed.
	if idx.streamBucketStatus[streamId][bucket] == STREAM_INACTIVE ||
		idx.streamBucketStatus[streamId][bucket] == STREAM_PREPARE_RECOVERY ||
		idx.streamBucketStatus[streamId][bucket] == STREAM_RECOVERY {
		logging.Debugf("Indexer::handleInitBuildDoneAck Skip InitBuildDoneAck %v %v %v",
			streamId, bucket, idx.streamBucketStatus[streamId][bucket])
		return
	}

	logging.Debugf("Indexer::handleInitBuildDoneAck StreamId %v Bucket %v",
		streamId, bucket)

	switch streamId {

	case common.INIT_STREAM:

		//send the ack to timekeeper
		idx.tkCmdCh <- msg
		<-idx.tkCmdCh

	default:
		logging.Debugf("Indexer::handleInitBuildDoneAck Unexpected Initial Build Ack Done "+
			"Received for Stream %v Bucket %v", streamId, bucket)
		common.CrashOnError(errors.New("Unexpected Initial Build Ack Done"))
	}

}

func (idx *indexer) handleMergeStreamAck(msg Message) {

	streamId := msg.(*MsgTKMergeStream).GetStreamId()
	bucket := msg.(*MsgTKMergeStream).GetBucket()
	mergeList := msg.(*MsgTKMergeStream).GetMergeList()

	logging.Debugf("Indexer::handleMergeStreamAck StreamId %v Bucket %v",
		streamId, bucket)

	switch streamId {

	case common.INIT_STREAM:
		delete(idx.streamBucketRequestStopCh[streamId], bucket)

		//skip processing merge ack for inactive or recovery streams.
		if idx.streamBucketStatus[streamId][bucket] == STREAM_INACTIVE ||
			idx.streamBucketStatus[streamId][bucket] == STREAM_PREPARE_RECOVERY ||
			idx.streamBucketStatus[streamId][bucket] == STREAM_RECOVERY {
			logging.Debugf("Indexer::handleMergeStreamAck Skip MergeStreamAck %v %v %v",
				streamId, bucket, idx.streamBucketStatus[streamId][bucket])
			return
		}

		idx.streamBucketStatus[streamId][bucket] = STREAM_INACTIVE

		//enable flush for this bucket in MAINT_STREAM
		//TODO shall this be moved to timekeeper now?
		idx.tkCmdCh <- &MsgTKToggleFlush{mType: TK_ENABLE_FLUSH,
			streamId: common.MAINT_STREAM,
			bucket:   bucket}
		<-idx.tkCmdCh

		//send the ack to timekeeper
		idx.tkCmdCh <- msg
		<-idx.tkCmdCh

		//for cbq bridge, return response after merge is done and
		//index is ready to query
		if !idx.enableManager {
			if clientCh, ok := idx.bucketCreateClientChMap[bucket]; ok {
				if clientCh != nil {
					clientCh <- &MsgSuccess{}
				}
				delete(idx.bucketCreateClientChMap, bucket)
			}
		} else {
			var instIdList []common.IndexInstId
			for _, inst := range mergeList {
				instIdList = append(instIdList, inst.InstId)
			}

			if err := idx.updateMetaInfoForIndexList(instIdList, true, true, false, false); err != nil {
				common.CrashOnError(err)
			}
		}

	default:
		logging.Debugf("Indexer::handleMergeStreamAck Unexpected Merge Stream Ack "+
			"Received for Stream %v Bucket %v", streamId, bucket)
		common.CrashOnError(errors.New("Unexpected Merge Stream Ack"))
	}

}

func (idx *indexer) handleStreamRequestDone(msg Message) {

	streamId := msg.(*MsgStreamInfo).GetStreamId()
	bucket := msg.(*MsgStreamInfo).GetBucket()
	buildTs := msg.(*MsgStreamInfo).GetBuildTs()

	logging.Debugf("Indexer::handleStreamRequestDone StreamId %v Bucket %v",
		streamId, bucket)

	//send the ack to timekeeper
	idx.tkCmdCh <- msg
	<-idx.tkCmdCh

	delete(idx.streamBucketRequestStopCh[streamId], bucket)
	idx.bucketBuildTs[bucket] = buildTs
}

func (idx *indexer) handleBucketNotFound(msg Message) {

	streamId := msg.(*MsgRecovery).GetStreamId()
	bucket := msg.(*MsgRecovery).GetBucket()

	logging.Debugf("Indexer::handleBucketNotFound StreamId %v Bucket %v",
		streamId, bucket)

	//If stream is inactive, no cleanup is required.
	//If stream is prepare_recovery, recovery will take care of
	//validating the bucket and taking corrective action.
	if idx.streamBucketStatus[streamId][bucket] == STREAM_INACTIVE ||
		idx.streamBucketStatus[streamId][bucket] == STREAM_PREPARE_RECOVERY {
		logging.Debugf("Indexer::handleBucketNotFound Skip %v %v %v",
			streamId, bucket, idx.streamBucketStatus[streamId][bucket])
		return
	}

	// delete index inst on the bucket from metadata repository and
	// return the list of deleted inst
	instIdList := idx.deleteIndexInstOnDeletedBucket(bucket, streamId)

	if len(instIdList) == 0 {
		logging.Debugf("Indexer::handleBucketNotFound Empty IndexList %v %v. Nothing to do.",
			streamId, bucket)
		return
	}

	idx.bulkUpdateState(instIdList, common.INDEX_STATE_DELETED)
	logging.Debugf("Indexer::handleBucketNotFound Updated Index State to DELETED %v",
		instIdList)

	msgUpdateIndexInstMap := idx.newIndexInstMsg(idx.indexInstMap)

	if err := idx.distributeIndexMapsToWorkers(msgUpdateIndexInstMap, nil); err != nil {
		common.CrashOnError(err)
	}

	idx.stopBucketStream(streamId, bucket)

	//cleanup index data for all indexes in the bucket
	for _, instId := range instIdList {
		index := idx.indexInstMap[instId]
		idx.cleanupIndexData(index, nil)
	}

	idx.streamBucketStatus[streamId][bucket] = STREAM_INACTIVE

	logging.Debugf("Indexer::handleBucketNotFound %v %v %v",
		streamId, bucket, idx.streamBucketStatus[streamId][bucket])

}

func (idx indexer) newIndexInstMsg(m common.IndexInstMap) *MsgUpdateInstMap {
	return &MsgUpdateInstMap{indexInstMap: m, stats: idx.stats.Clone()}
}

func (idx *indexer) cleanupIndexData(indexInst common.IndexInst,
	clientCh MsgChannel) {

	indexInstId := indexInst.InstId
	idxPartnInfo := idx.indexPartnMap[indexInstId]

	//update internal maps
	delete(idx.indexInstMap, indexInstId)
	delete(idx.indexPartnMap, indexInstId)

	msgUpdateIndexInstMap := idx.newIndexInstMsg(idx.indexInstMap)
	msgUpdateIndexPartnMap := &MsgUpdatePartnMap{indexPartnMap: idx.indexPartnMap}

	if err := idx.distributeIndexMapsToWorkers(msgUpdateIndexInstMap, msgUpdateIndexPartnMap); err != nil {
		if clientCh != nil {
			clientCh <- &MsgError{
				err: Error{code: ERROR_INDEXER_INTERNAL_ERROR,
					severity: FATAL,
					cause:    err,
					category: INDEXER}}
		}
		common.CrashOnError(err)
	}

	//for all partitions managed by this indexer
	for _, partnInst := range idxPartnInfo {
		sc := partnInst.Sc
		//close all the slices
		for _, slice := range sc.GetAllSlices() {
			slice.Close()
			//wipe the physical files
			slice.Destroy()
		}
	}

}

func (idx *indexer) cleanupIndex(indexInst common.IndexInst,
	clientCh MsgChannel) {

	idx.cleanupIndexData(indexInst, clientCh)

	//send Stream update to workers
	if ok := idx.sendStreamUpdateForDropIndex(indexInst, clientCh); !ok {
		return
	}

	clientCh <- &MsgSuccess{}
}

func (idx *indexer) shutdownWorkers() {

	//shutdown mutation manager
	idx.mutMgrCmdCh <- &MsgGeneral{mType: MUT_MGR_SHUTDOWN}
	<-idx.mutMgrCmdCh

	//shutdown scan coordinator
	idx.scanCoordCmdCh <- &MsgGeneral{mType: SCAN_COORD_SHUTDOWN}
	<-idx.scanCoordCmdCh

	//shutdown storage manager
	idx.storageMgrCmdCh <- &MsgGeneral{mType: STORAGE_MGR_SHUTDOWN}
	<-idx.storageMgrCmdCh

	//shutdown timekeeper
	idx.tkCmdCh <- &MsgGeneral{mType: TK_SHUTDOWN}
	<-idx.tkCmdCh

	//shutdown admin manager
	idx.adminMgrCmdCh <- &MsgGeneral{mType: ADMIN_MGR_SHUTDOWN}
	<-idx.adminMgrCmdCh

	if idx.enableManager {
		//shutdown cluster manager
		idx.clustMgrAgentCmdCh <- &MsgGeneral{mType: CLUST_MGR_AGENT_SHUTDOWN}
		<-idx.clustMgrAgentCmdCh
	}

	//shutdown kv sender
	idx.kvSenderCmdCh <- &MsgGeneral{mType: KV_SENDER_SHUTDOWN}
	<-idx.kvSenderCmdCh
}

func (idx *indexer) Shutdown() Message {

	logging.Infof("Indexer::Shutdown -  Shutting Down")
	//close the internal shutdown channel
	close(idx.shutdownInitCh)
	<-idx.shutdownCompleteCh
	logging.Infof("Indexer:Shutdown - Shutdown Complete")
	return nil
}

func (idx *indexer) sendStreamUpdateForBuildIndex(instIdList []common.IndexInstId,
	buildStream common.StreamId, bucket string, buildTs Timestamp, clientCh MsgChannel) bool {

	var cmd Message
	var indexList []common.IndexInst
	var bucketUUIDList []string
	for _, instId := range instIdList {
		indexInst := idx.indexInstMap[instId]
		indexList = append(indexList, indexInst)
		bucketUUIDList = append(bucketUUIDList, indexInst.Defn.BucketUUID)
	}

	respCh := make(MsgChannel)

	cmd = &MsgStreamUpdate{mType: OPEN_STREAM,
		streamId:  buildStream,
		bucket:    bucket,
		indexList: indexList,
		buildTs:   buildTs,
		respCh:    respCh,
		restartTs: nil}

	//send stream update to timekeeper
	if resp := idx.sendStreamUpdateToWorker(cmd, idx.tkCmdCh,
		"Timekeeper"); resp.GetMsgType() != MSG_SUCCESS {
		if clientCh != nil {
			clientCh <- resp
		}
		respErr := resp.(*MsgError).GetError()
		common.CrashOnError(respErr.cause)
	}

	//send stream update to mutation manager
	if resp := idx.sendStreamUpdateToWorker(cmd, idx.mutMgrCmdCh,
		"MutationMgr"); resp.GetMsgType() != MSG_SUCCESS {
		if clientCh != nil {
			clientCh <- resp
		}
		respErr := resp.(*MsgError).GetError()
		common.CrashOnError(respErr.cause)
	}

	stopCh := make(StopChannel)

	if _, ok := idx.streamBucketRequestStopCh[buildStream][bucket]; !ok {
		idx.streamBucketRequestStopCh[buildStream] = make(BucketRequestStopCh)
	}
	idx.streamBucketRequestStopCh[buildStream][bucket] = stopCh

	clustAddr := idx.config["clusterAddr"].String()
	numVb := idx.config["numVbuckets"].Int()

	reqLock := idx.acquireStreamRequestLock(bucket, buildStream)
	go func(reqLock *kvRequest) {
		defer idx.releaseStreamRequestLock(reqLock)
		idx.waitStreamRequestLock(reqLock)

	retryloop:
		for {
			if !ValidateBucket(clustAddr, bucket, bucketUUIDList) {
				logging.Errorf("Indexer::sendStreamUpdateForBuildIndex \n\tBucket Not Found "+
					"For Stream %v Bucket %v", buildStream, bucket)
				idx.internalRecvCh <- &MsgRecovery{mType: INDEXER_BUCKET_NOT_FOUND,
					streamId: buildStream,
					bucket:   bucket}
				break retryloop
			}
			idx.sendMsgToKVSender(cmd)

			if resp, ok := <-respCh; ok {

				switch resp.GetMsgType() {

				case MSG_SUCCESS_OPEN_STREAM:
					logging.Debugf("Indexer::sendStreamUpdateForBuildIndex Stream Request Success For "+
						"Stream %v Bucket %v.", buildStream, bucket)

					//once stream request is successful re-calculate the KV timestamp.
					//This makes sure indexer doesn't use a timestamp which can never
					//be caught up to (due to kv rollback).
					//if there is a failover after this, it will be observed as a rollback
					buildTs, err := computeBucketBuildTs(clustAddr, bucket, numVb)
					if err == nil {
						idx.internalRecvCh <- &MsgStreamInfo{mType: STREAM_REQUEST_DONE,
							streamId: buildStream,
							bucket:   bucket,
							buildTs:  buildTs,
							activeTs: resp.(*MsgSuccessOpenStream).GetActiveTs(),
						}
						break retryloop
					}

				case INDEXER_ROLLBACK:
					//an initial build request should never receive rollback message
					logging.Errorf("Indexer::sendStreamUpdateForBuildIndex Unexpected Rollback from "+
						"Projector during Initial Stream Request %v", resp)
					common.CrashOnError(ErrKVRollbackForInitRequest)

				default:
					//log and retry for all other responses
					respErr := resp.(*MsgError).GetError()
					logging.Errorf("Indexer::sendStreamUpdateForBuildIndex Stream %v Bucket %v."+
						"Error from Projector %v. Retrying.", buildStream, bucket, respErr.cause)
					time.Sleep(KV_RETRY_INTERVAL * time.Millisecond)

				}
			}
		}
	}(reqLock)

	return true

}

func (idx *indexer) sendMsgToKVSender(cmd Message) {
	idx.kvlock.Lock()
	defer idx.kvlock.Unlock()

	//send stream update to kv sender
	idx.kvSenderCmdCh <- cmd
	<-idx.kvSenderCmdCh
}

func (idx *indexer) sendStreamUpdateToWorker(cmd Message, workerCmdCh MsgChannel,
	workerStr string) Message {

	//send message to worker
	workerCmdCh <- cmd
	if resp, ok := <-workerCmdCh; ok {
		if resp.GetMsgType() != MSG_SUCCESS {

			logging.Errorf("Indexer::sendStreamUpdateToWorker - Error received from %v "+
				"processing Msg %v Err %v. Aborted.", workerStr, cmd, resp)

			return &MsgError{
				err: Error{code: ERROR_INDEXER_INTERNAL_ERROR,
					severity: FATAL,
					cause:    ErrInconsistentState,
					category: INDEXER}}
		}
	} else {
		logging.Errorf("Indexer::sendStreamUpdateToWorker - Error communicating with %v "+
			"processing Msg %v Err %v. Aborted.", workerStr, cmd, resp)

		return &MsgError{
			err: Error{code: ERROR_INDEXER_INTERNAL_ERROR,
				severity: FATAL,
				cause:    ErrFatalComm,
				category: INDEXER}}
	}
	return &MsgSuccess{}
}

func (idx *indexer) sendStreamUpdateForDropIndex(indexInst common.IndexInst,
	clientCh MsgChannel) bool {

	var cmd Message
	var indexList []common.IndexInst
	indexList = append(indexList, indexInst)

	var indexStreamIds []common.StreamId

	//index in INIT_STREAM needs to be removed from MAINT_STREAM as well
	//if the state is CATCHUP
	switch indexInst.Stream {

	case common.MAINT_STREAM:
		indexStreamIds = append(indexStreamIds, common.MAINT_STREAM)

	case common.INIT_STREAM:
		indexStreamIds = append(indexStreamIds, common.INIT_STREAM)
		if indexInst.State == common.INDEX_STATE_CATCHUP {
			indexStreamIds = append(indexStreamIds, common.MAINT_STREAM)
		}

	default:
		logging.Errorf("Indexer::sendStreamUpdateForDropIndex \n\t Unsupported StreamId %v", indexInst.Stream)
		common.CrashOnError(ErrInvalidStream)
	}

	for _, streamId := range indexStreamIds {

		respCh := make(MsgChannel)

		if idx.checkBucketExistsInStream(indexInst.Defn.Bucket, streamId, false) {

			cmd = &MsgStreamUpdate{mType: REMOVE_INDEX_LIST_FROM_STREAM,
				streamId:  streamId,
				indexList: indexList,
				respCh:    respCh}
		} else {
			if idx.checkLastBucketInStream(indexInst.Defn.Bucket, streamId) {
				//promote to close stream
				cmd = &MsgStreamUpdate{mType: CLOSE_STREAM,
					streamId: streamId,
					bucket:   indexInst.Defn.Bucket,
					respCh:   respCh}

			} else {
				cmd = &MsgStreamUpdate{mType: REMOVE_BUCKET_FROM_STREAM,
					streamId: streamId,
					bucket:   indexInst.Defn.Bucket,
					respCh:   respCh}
			}
			idx.streamBucketStatus[streamId][indexInst.Defn.Bucket] = STREAM_INACTIVE
		}

		//send stream update to mutation manager
		if resp := idx.sendStreamUpdateToWorker(cmd, idx.mutMgrCmdCh,
			"MutationMgr"); resp.GetMsgType() != MSG_SUCCESS {
			if clientCh != nil {
				clientCh <- resp
			}
			respErr := resp.(*MsgError).GetError()
			common.CrashOnError(respErr.cause)
		}

		//send stream update to timekeeper
		if resp := idx.sendStreamUpdateToWorker(cmd, idx.tkCmdCh,
			"Timekeeper"); resp.GetMsgType() != MSG_SUCCESS {
			if clientCh != nil {
				clientCh <- resp
			}
			respErr := resp.(*MsgError).GetError()
			common.CrashOnError(respErr.cause)
		}
		clustAddr := idx.config["clusterAddr"].String()

		reqLock := idx.acquireStreamRequestLock(indexInst.Defn.Bucket, streamId)
		go func(reqLock *kvRequest) {
			defer idx.releaseStreamRequestLock(reqLock)
			idx.waitStreamRequestLock(reqLock)
		retryloop:
			for {

				if !ValidateBucket(clustAddr, indexInst.Defn.Bucket, []string{indexInst.Defn.BucketUUID}) {
					logging.Errorf("Indexer::sendStreamUpdateForDropIndex \n\tBucket Not Found "+
						"For Stream %v Bucket %v", streamId, indexInst.Defn.Bucket)
					idx.internalRecvCh <- &MsgRecovery{mType: INDEXER_BUCKET_NOT_FOUND,
						streamId: streamId,
						bucket:   indexInst.Defn.Bucket}
					break retryloop
				}

				idx.sendMsgToKVSender(cmd)

				if resp, ok := <-respCh; ok {

					switch resp.GetMsgType() {

					case MSG_SUCCESS:
						break retryloop

					default:
						//log and retry for all other responses
						respErr := resp.(*MsgError).GetError()
						logging.Errorf("Indexer::sendStreamUpdateForDropIndex - Stream %v Bucket %v"+
							"Error from Projector %v. Retrying.", streamId, indexInst.Defn.Bucket, respErr.cause)
						time.Sleep(KV_RETRY_INTERVAL * time.Millisecond)

					}
				}
			}
		}(reqLock)
	}

	return true

}

func (idx *indexer) initPartnInstance(indexInst common.IndexInst,
	respCh MsgChannel) (PartitionInstMap, error) {

	//initialize partitionInstMap for this index
	partnInstMap := make(PartitionInstMap)

	//get all partitions for this index
	partnDefnList := indexInst.Pc.GetAllPartitions()

	for i, partnDefn := range partnDefnList {
		//TODO: Ignore partitions which do not belong to this
		//indexer node(based on the endpoints)
		partnInst := PartitionInst{Defn: partnDefn,
			Sc: NewHashedSliceContainer()}

		logging.Infof("Indexer::initPartnInstance Initialized Partition: \n\t Index: %v Partition: %v",
			indexInst.InstId, partnInst)

		//add a single slice per partition for now
		if slice, err := NewSlice(SliceId(0), &indexInst, idx.config, idx.stats); err == nil {
			partnInst.Sc.AddSlice(0, slice)
			logging.Infof("Indexer::initPartnInstance Initialized Slice: \n\t Index: %v Slice: %v",
				indexInst.InstId, slice)

			partnInstMap[common.PartitionId(i)] = partnInst
		} else {
			logging.Errorf("Indexer::initPartnInstance Error creating slice %v. Abort.",
				err)

			if respCh != nil {
				respCh <- &MsgError{
					err: Error{code: ERROR_INDEXER_INTERNAL_ERROR,
						severity: FATAL,
						cause:    errors.New("Indexer Internal Error"),
						category: INDEXER}}
				return nil, err
			}
		}
	}

	return partnInstMap, nil
}

func (idx *indexer) distributeIndexMapsToWorkers(msgUpdateIndexInstMap Message,
	msgUpdateIndexPartnMap Message) error {

	//update index map in storage manager
	if err := idx.sendUpdatedIndexMapToWorker(msgUpdateIndexInstMap, msgUpdateIndexPartnMap, idx.storageMgrCmdCh,
		"StorageMgr"); err != nil {
		return err
	}

	//update index map in mutation manager
	if err := idx.sendUpdatedIndexMapToWorker(msgUpdateIndexInstMap, msgUpdateIndexPartnMap, idx.mutMgrCmdCh,
		"MutationMgr"); err != nil {
		return err
	}

	//update index map in scan coordinator
	if err := idx.sendUpdatedIndexMapToWorker(msgUpdateIndexInstMap, msgUpdateIndexPartnMap, idx.scanCoordCmdCh,
		"ScanCoordinator"); err != nil {
		return err
	}

	if err := idx.sendUpdatedIndexMapToWorker(msgUpdateIndexInstMap, msgUpdateIndexPartnMap, idx.tkCmdCh,
		"Timekeeper"); err != nil {
		return err
	}

	if err := idx.sendUpdatedIndexMapToWorker(msgUpdateIndexInstMap, nil, idx.statsMgrCmdCh,
		"statsMgr"); err != nil {
		return err
	}
	return nil
}

func (idx *indexer) sendUpdatedIndexMapToWorker(msgUpdateIndexInstMap Message,
	msgUpdateIndexPartnMap Message, workerCmdCh chan Message, workerStr string) error {

	if msgUpdateIndexInstMap != nil {
		workerCmdCh <- msgUpdateIndexInstMap

		if resp, ok := <-workerCmdCh; ok {

			if resp.GetMsgType() == MSG_ERROR {
				logging.Errorf("Indexer::sendUpdatedIndexMapToWorker - Error received from %v processing "+
					"Msg %v Err %v. Aborted.", workerStr, msgUpdateIndexInstMap, resp)
				respErr := resp.(*MsgError).GetError()
				return respErr.cause
			}
		} else {
			logging.Errorf("Indexer::sendUpdatedIndexMapToWorker - Error communicating with %v "+
				"processing Msg %v. Aborted.", workerStr, msgUpdateIndexInstMap)
			return ErrFatalComm
		}
	}

	if msgUpdateIndexPartnMap != nil {
		workerCmdCh <- msgUpdateIndexPartnMap
		if resp, ok := <-workerCmdCh; ok {

			if resp.GetMsgType() == MSG_ERROR {
				logging.Errorf("Indexer::sendUpdatedIndexMapToWorker - Error received from %v processing "+
					"Msg %v Err %v. Aborted.", workerStr, msgUpdateIndexPartnMap, resp)
				respErr := resp.(*MsgError).GetError()
				return respErr.cause
			}
		} else {
			logging.Errorf("Indexer::sendUpdatedIndexMapToWorker - Error communicating with %v "+
				"processing Msg %v. Aborted.", workerStr, msgUpdateIndexPartnMap, resp)
			return ErrFatalComm
		}
	}

	return nil

}

func (idx *indexer) initStreamAddressMap() {
	StreamAddrMap = make(StreamAddressMap)

	port2addr := func(p string) string {
		return net.JoinHostPort("", idx.config[p].String())
	}

	StreamAddrMap[common.MAINT_STREAM] = common.Endpoint(port2addr("streamMaintPort"))
	StreamAddrMap[common.CATCHUP_STREAM] = common.Endpoint(port2addr("streamCatchupPort"))
	StreamAddrMap[common.INIT_STREAM] = common.Endpoint(port2addr("streamInitPort"))
}

func (idx *indexer) initStreamTopicName() {
	StreamTopicName = make(map[common.StreamId]string)

	StreamTopicName[common.MAINT_STREAM] = MAINT_TOPIC + "_" + idx.id
	StreamTopicName[common.CATCHUP_STREAM] = CATCHUP_TOPIC + "_" + idx.id
	StreamTopicName[common.INIT_STREAM] = INIT_TOPIC + "_" + idx.id
}

//checkDuplicateIndex checks if an index with the given indexInstId
// or name already exists
func (idx *indexer) checkDuplicateIndex(indexInst common.IndexInst,
	respCh MsgChannel) bool {

	//if the indexInstId already exists, return error
	if index, ok := idx.indexInstMap[indexInst.InstId]; ok {
		logging.Errorf("Indexer::checkDuplicateIndex Duplicate Index Instance. "+
			"IndexInstId: %v, Index: %v", indexInst.InstId, index)

		if respCh != nil {
			respCh <- &MsgError{
				err: Error{code: ERROR_INDEX_ALREADY_EXISTS,
					severity: FATAL,
					cause:    errors.New("Duplicate Index Instance"),
					category: INDEXER}}
		}
		return false
	}

	//if the index name already exists for the same bucket,
	//return error
	for _, index := range idx.indexInstMap {

		if index.Defn.Name == indexInst.Defn.Name &&
			index.Defn.Bucket == indexInst.Defn.Bucket &&
			index.State != common.INDEX_STATE_DELETED {

			logging.Errorf("Indexer::checkDuplicateIndex Duplicate Index Name. "+
				"Name: %v, Duplicate Index: %v", indexInst.Defn.Name, index)

			if respCh != nil {
				respCh <- &MsgError{
					err: Error{code: ERROR_INDEX_ALREADY_EXISTS,
						severity: FATAL,
						cause:    errors.New("Duplicate Index Name"),
						category: INDEXER}}
			}
			return false
		}

	}
	return true
}

//checkDuplicateInitialBuildRequest check if any other index on the given bucket
//is already building
func (idx *indexer) checkDuplicateInitialBuildRequest(bucket string,
	instIdList []common.IndexInstId, respCh MsgChannel, errMap map[common.IndexInstId]error) bool {

	//if initial build is already running for some other index on this bucket,
	//cannot start another one
	for _, index := range idx.indexInstMap {

		if ((index.State == common.INDEX_STATE_INITIAL ||
			index.State == common.INDEX_STATE_CATCHUP) &&
			index.Defn.Bucket == bucket) ||
			idx.checkStreamRequestPending(index.Stream, bucket) {

			errStr := fmt.Sprintf("Build Already In Progress. Bucket %v", bucket)
			if idx.enableManager {
				idx.bulkUpdateError(instIdList, errStr)
				for _, instId := range instIdList {
					errMap[instId] = errors.New(errStr)
				}
			} else if respCh != nil {
				respCh <- &MsgError{
					err: Error{code: ERROR_INDEX_BUILD_IN_PROGRESS,
						severity: FATAL,
						cause:    errors.New(errStr),
						category: INDEXER}}
			}
			return false
		}
	}

	return true
}

//TODO If this function gets error before its finished, the state
//can be inconsistent. This needs to be fixed.
func (idx *indexer) handleInitialBuildDone(msg Message) {

	bucket := msg.(*MsgTKInitBuildDone).GetBucket()
	streamId := msg.(*MsgTKInitBuildDone).GetStreamId()

	//skip processing initial build done for inactive or recovery streams.
	//the streams would be restarted and then build done would get recomputed.
	if idx.streamBucketStatus[streamId][bucket] == STREAM_INACTIVE ||
		idx.streamBucketStatus[streamId][bucket] == STREAM_PREPARE_RECOVERY ||
		idx.streamBucketStatus[streamId][bucket] == STREAM_RECOVERY {
		logging.Debugf("Indexer::handleInitialBuildDone Skip InitBuildDone %v %v %v",
			streamId, bucket, idx.streamBucketStatus[streamId][bucket])
		return
	}

	logging.Debugf("Indexer::handleInitialBuildDone Bucket: %v Stream: %v", bucket, streamId)

	//MAINT_STREAM should already be running for this bucket,
	//as first index gets added to MAINT_STREAM always
	if idx.checkBucketExistsInStream(bucket, common.MAINT_STREAM, true) == false {
		logging.Errorf("Indexer::handleInitialBuildDone MAINT_STREAM not enabled for Bucket: %v. "+
			"Cannot Process Initial Build Done.", bucket)
		common.CrashOnError(ErrMaintStreamMissingBucket)
	}

	//get the list of indexes for this bucket and stream in INITIAL state
	var indexList []common.IndexInst
	var instIdList []common.IndexInstId
	var bucketUUIDList []string
	for _, index := range idx.indexInstMap {
		if index.Defn.Bucket == bucket && index.Stream == streamId &&
			index.State == common.INDEX_STATE_INITIAL {
			//index in INIT_STREAM move to Catchup state
			if streamId == common.INIT_STREAM {
				index.State = common.INDEX_STATE_CATCHUP
			} else {
				index.State = common.INDEX_STATE_ACTIVE
			}
			indexList = append(indexList, index)
			instIdList = append(instIdList, index.InstId)
			bucketUUIDList = append(bucketUUIDList, index.Defn.BucketUUID)
		}
	}

	if len(instIdList) == 0 {
		logging.Debugf("Indexer::handleInitialBuildDone Empty IndexList %v %v. Nothing to do.",
			streamId, bucket)
		return
	}

	//update the IndexInstMap
	for _, index := range indexList {
		idx.indexInstMap[index.InstId] = index
	}

	//send updated maps to all workers
	msgUpdateIndexInstMap := idx.newIndexInstMsg(idx.indexInstMap)

	if err := idx.distributeIndexMapsToWorkers(msgUpdateIndexInstMap, nil); err != nil {
		common.CrashOnError(err)
	}

	if err := idx.updateMetaInfoForIndexList(instIdList, true, true, false, false); err != nil {
		common.CrashOnError(err)
	}

	//if index is already in MAINT_STREAM, nothing more needs to be done
	if streamId == common.MAINT_STREAM {

		//for cbq bridge, return response as index is ready to query
		if !idx.enableManager {
			if clientCh, ok := idx.bucketCreateClientChMap[bucket]; ok {
				if clientCh != nil {
					clientCh <- &MsgSuccess{}
				}
				delete(idx.bucketCreateClientChMap, bucket)
			}
		}
		return
	}

	//Add index to MAINT_STREAM in Catchup State,
	//so mutations for this index are already in queue to
	//allow convergence with INIT_STREAM.
	respCh := make(MsgChannel)
	stopCh := make(StopChannel)

	cmd := &MsgStreamUpdate{mType: ADD_INDEX_LIST_TO_STREAM,
		streamId:  common.MAINT_STREAM,
		bucket:    bucket,
		indexList: indexList,
		respCh:    respCh,
		stopCh:    stopCh}

	//send stream update to timekeeper
	if resp := idx.sendStreamUpdateToWorker(cmd, idx.tkCmdCh, "Timekeeper"); resp.GetMsgType() != MSG_SUCCESS {
		respErr := resp.(*MsgError).GetError()
		common.CrashOnError(respErr.cause)
	}

	//send stream update to mutation manager
	if resp := idx.sendStreamUpdateToWorker(cmd, idx.mutMgrCmdCh, "MutationMgr"); resp.GetMsgType() != MSG_SUCCESS {
		respErr := resp.(*MsgError).GetError()
		common.CrashOnError(respErr.cause)
	}

	clustAddr := idx.config["clusterAddr"].String()

	reqLock := idx.acquireStreamRequestLock(bucket, streamId)
	go func(reqLock *kvRequest) {
		defer idx.releaseStreamRequestLock(reqLock)
		idx.waitStreamRequestLock(reqLock)
	retryloop:
		for {
			if !ValidateBucket(clustAddr, bucket, bucketUUIDList) {
				logging.Errorf("Indexer::handleInitialBuildDone \n\tBucket Not Found "+
					"For Stream %v Bucket %v", streamId, bucket)
				break retryloop
			}
			idx.sendMsgToKVSender(cmd)

			if resp, ok := <-respCh; ok {

				switch resp.GetMsgType() {

				case MSG_SUCCESS:
					mergeTs := resp.(*MsgStreamUpdate).GetRestartTs()

					idx.internalRecvCh <- &MsgTKInitBuildDone{
						mType:    TK_INIT_BUILD_DONE_ACK,
						streamId: streamId,
						bucket:   bucket,
						mergeTs:  mergeTs}
					break retryloop

				default:
					//log and retry for all other responses
					respErr := resp.(*MsgError).GetError()

					//If projector returns TopicMissing/GenServerClosed, AddInstance
					//cannot succeed. This needs to be aborted so that stream lock is
					//released and MTR can proceed to repair the Topic.
					if respErr.cause.Error() == common.ErrorClosed.Error() ||
						respErr.cause.Error() == projClient.ErrorTopicMissing.Error() {
						logging.Warnf("Indexer::handleInitialBuildDone Stream %v Bucket %v "+
							"Error from Projector %v. Aborting.", streamId, bucket, respErr.cause)
						break retryloop
					} else {

						logging.Errorf("Indexer::handleInitialBuildDone Stream %v Bucket %v "+
							"Error from Projector %v. Retrying.", streamId, bucket, respErr.cause)
						time.Sleep(KV_RETRY_INTERVAL * time.Millisecond)
					}
				}
			}
		}
	}(reqLock)

}

func (idx *indexer) handleMergeStream(msg Message) {

	bucket := msg.(*MsgTKMergeStream).GetBucket()
	streamId := msg.(*MsgTKMergeStream).GetStreamId()

	//skip processing stream merge for inactive or recovery streams.
	if idx.streamBucketStatus[streamId][bucket] == STREAM_INACTIVE ||
		idx.streamBucketStatus[streamId][bucket] == STREAM_PREPARE_RECOVERY ||
		idx.streamBucketStatus[streamId][bucket] == STREAM_RECOVERY {
		logging.Debugf("Indexer::handleMergeStream Skip MergeStream %v %v %v",
			streamId, bucket, idx.streamBucketStatus[streamId][bucket])
		return
	}

	//MAINT_STREAM should already be running for this bucket,
	//as first index gets added to MAINT_STREAM always
	if idx.checkBucketExistsInStream(bucket, common.MAINT_STREAM, true) == false {
		logging.Errorf("Indexer::handleMergeStream \n\tMAINT_STREAM not enabled for Bucket: %v ."+
			"Cannot Process Merge Stream", bucket)
		common.CrashOnError(ErrMaintStreamMissingBucket)
	}

	switch streamId {

	case common.INIT_STREAM:
		idx.handleMergeInitStream(msg)

	default:
		logging.Errorf("Indexer::handleMergeStream \n\tOnly INIT_STREAM can be merged "+
			"to MAINT_STREAM. Found Stream: %v.", streamId)
		common.CrashOnError(ErrInvalidStream)
	}
}

//TODO If this function gets error before its finished, the state
//can be inconsistent. This needs to be fixed.
func (idx *indexer) handleMergeInitStream(msg Message) {

	bucket := msg.(*MsgTKMergeStream).GetBucket()
	streamId := msg.(*MsgTKMergeStream).GetStreamId()

	logging.Debugf("Indexer::handleMergeInitStream Bucket: %v Stream: %v", bucket, streamId)

	//get the list of indexes for this bucket in CATCHUP state
	var indexList []common.IndexInst
	var bucketUUIDList []string
	for _, index := range idx.indexInstMap {
		if index.Defn.Bucket == bucket && index.Stream == streamId &&
			index.State == common.INDEX_STATE_CATCHUP {

			index.State = common.INDEX_STATE_ACTIVE
			index.Stream = common.MAINT_STREAM
			indexList = append(indexList, index)
			bucketUUIDList = append(bucketUUIDList, index.Defn.BucketUUID)
		}
	}

	//update the IndexInstMap
	for _, index := range indexList {
		idx.indexInstMap[index.InstId] = index
	}

	//send updated maps to all workers
	msgUpdateIndexInstMap := idx.newIndexInstMsg(idx.indexInstMap)

	if err := idx.distributeIndexMapsToWorkers(msgUpdateIndexInstMap, nil); err != nil {
		common.CrashOnError(err)
	}

	respCh := make(MsgChannel)
	stopCh := make(StopChannel)

	//remove bucket from INIT_STREAM
	var cmd Message
	if idx.checkLastBucketInStream(bucket, streamId) {
		//promote to close stream
		cmd = &MsgStreamUpdate{mType: CLOSE_STREAM,
			streamId: streamId,
			bucket:   bucket,
			respCh:   respCh,
			stopCh:   stopCh}
	} else {
		cmd = &MsgStreamUpdate{mType: REMOVE_BUCKET_FROM_STREAM,
			streamId: streamId,
			bucket:   bucket,
			respCh:   respCh,
			stopCh:   stopCh,
		}
	}

	//send stream update to timekeeper
	if resp := idx.sendStreamUpdateToWorker(cmd, idx.tkCmdCh, "Timekeeper"); resp.GetMsgType() != MSG_SUCCESS {
		respErr := resp.(*MsgError).GetError()
		common.CrashOnError(respErr.cause)
	}

	//send stream update to mutation manager
	if resp := idx.sendStreamUpdateToWorker(cmd, idx.mutMgrCmdCh, "MutationMgr"); resp.GetMsgType() != MSG_SUCCESS {
		respErr := resp.(*MsgError).GetError()
		common.CrashOnError(respErr.cause)
	}

	if _, ok := idx.streamBucketRequestStopCh[streamId][bucket]; !ok {
		idx.streamBucketRequestStopCh[streamId] = make(BucketRequestStopCh)
	}
	idx.streamBucketRequestStopCh[streamId][bucket] = stopCh

	clustAddr := idx.config["clusterAddr"].String()

	reqLock := idx.acquireStreamRequestLock(bucket, streamId)
	go func(reqLock *kvRequest) {
		defer idx.releaseStreamRequestLock(reqLock)
		idx.waitStreamRequestLock(reqLock)
	retryloop:
		for {
			if !ValidateBucket(clustAddr, bucket, bucketUUIDList) {
				logging.Errorf("Indexer::handleMergeInitStream Bucket Not Found "+
					"For Stream %v Bucket %v", streamId, bucket)
				break retryloop
			}
			idx.sendMsgToKVSender(cmd)

			if resp, ok := <-respCh; ok {

				switch resp.GetMsgType() {

				case MSG_SUCCESS:
					idx.internalRecvCh <- &MsgTKMergeStream{
						mType:     TK_MERGE_STREAM_ACK,
						streamId:  streamId,
						bucket:    bucket,
						mergeList: indexList}
					break retryloop

				default:
					//log and retry for all other responses
					respErr := resp.(*MsgError).GetError()
					logging.Errorf("Indexer::handleMergeInitStream Stream %v Bucket %v "+
						"Error from Projector %v. Retrying.", streamId, bucket, respErr.cause)
					time.Sleep(KV_RETRY_INTERVAL * time.Millisecond)
				}
			}
		}
	}(reqLock)

	logging.Debugf("Indexer::handleMergeInitStream Merge Done Bucket: %v Stream: %v",
		bucket, streamId)
}

//checkBucketExistsInStream returns true if there is no index in the given stream
//which belongs to the given bucket, else false
func (idx *indexer) checkBucketExistsInStream(bucket string, streamId common.StreamId, checkDelete bool) bool {

	//check if any index of the given bucket is in the Stream
	for _, index := range idx.indexInstMap {

		// use checkDelete to verify index in DELETED status.   If an index is dropped while
		// there is concurrent build, the stream will not be cleaned up.
		if index.Defn.Bucket == bucket && index.Stream == streamId &&
			(index.State == common.INDEX_STATE_ACTIVE ||
				index.State == common.INDEX_STATE_CATCHUP ||
				index.State == common.INDEX_STATE_INITIAL ||
				(index.State == common.INDEX_STATE_DELETED && checkDelete)) {
			return true
		}
	}

	return false

}

//checkLastBucketInStream returns true if the given bucket is the only bucket
//active in the given stream, else false
func (idx *indexer) checkLastBucketInStream(bucket string, streamId common.StreamId) bool {

	for _, index := range idx.indexInstMap {

		if index.Defn.Bucket != bucket && index.Stream == streamId &&
			(index.State == common.INDEX_STATE_ACTIVE ||
				index.State == common.INDEX_STATE_CATCHUP ||
				index.State == common.INDEX_STATE_INITIAL) {
			return false
		}
	}

	return true

}

//checkStreamEmpty return true if there is no index currently in the
//give stream, else false
func (idx *indexer) checkStreamEmpty(streamId common.StreamId) bool {

	for _, index := range idx.indexInstMap {
		if index.Stream == streamId {
			logging.Tracef("Indexer::checkStreamEmpty Found Index %v Stream %v",
				index.InstId, streamId)
			return false
		}
	}
	logging.Tracef("Indexer::checkStreamEmpty Stream %v Empty", streamId)

	return true

}

func (idx *indexer) getIndexListForBucketAndStream(streamId common.StreamId,
	bucket string) []common.IndexInst {

	indexList := make([]common.IndexInst, 0)
	for _, idx := range idx.indexInstMap {

		if idx.Stream == streamId && idx.Defn.Bucket == bucket {

			indexList = append(indexList, idx)

		}
	}

	return indexList

}

func (idx *indexer) stopBucketStream(streamId common.StreamId, bucket string) {

	logging.Debugf("Indexer::stopBucketStream Stream: %v Bucket %v", streamId, bucket)

	respCh := make(MsgChannel)
	stopCh := make(StopChannel)

	var cmd Message
	if idx.checkLastBucketInStream(bucket, streamId) {
		//promote to close stream
		cmd = &MsgStreamUpdate{mType: CLOSE_STREAM,
			streamId: streamId,
			bucket:   bucket,
			respCh:   respCh,
			stopCh:   stopCh}
	} else {
		cmd = &MsgStreamUpdate{mType: REMOVE_BUCKET_FROM_STREAM,
			streamId: streamId,
			bucket:   bucket,
			respCh:   respCh,
			stopCh:   stopCh}
	}

	//send stream update to mutation manager
	if resp := idx.sendStreamUpdateToWorker(cmd, idx.mutMgrCmdCh,
		"MutationMgr"); resp.GetMsgType() != MSG_SUCCESS {
		respErr := resp.(*MsgError).GetError()
		common.CrashOnError(respErr.cause)
	}

	//send stream update to timekeeper
	if resp := idx.sendStreamUpdateToWorker(cmd, idx.tkCmdCh,
		"Timekeeper"); resp.GetMsgType() != MSG_SUCCESS {
		respErr := resp.(*MsgError).GetError()
		common.CrashOnError(respErr.cause)
	}

	if _, ok := idx.streamBucketRequestStopCh[streamId][bucket]; !ok {
		idx.streamBucketRequestStopCh[streamId] = make(BucketRequestStopCh)
	}
	idx.streamBucketRequestStopCh[streamId][bucket] = stopCh

	reqLock := idx.acquireStreamRequestLock(bucket, streamId)
	go func(reqLock *kvRequest) {
		defer idx.releaseStreamRequestLock(reqLock)
		idx.waitStreamRequestLock(reqLock)
	retryloop:
		for {

			idx.sendMsgToKVSender(cmd)

			if resp, ok := <-respCh; ok {

				switch resp.GetMsgType() {

				case MSG_SUCCESS:
					idx.internalRecvCh <- &MsgRecovery{mType: INDEXER_PREPARE_DONE,
						streamId: streamId,
						bucket:   bucket}
					break retryloop

				default:
					//log and retry for all other responses
					respErr := resp.(*MsgError).GetError()
					logging.Errorf("Indexer::stopBucketStream Stream %v Bucket %v "+
						"Error from Projector %v. Retrying.", streamId, bucket, respErr.cause)
					time.Sleep(KV_RETRY_INTERVAL * time.Millisecond)

				}
			}
		}
	}(reqLock)
}

func (idx *indexer) startBucketStream(streamId common.StreamId, bucket string,
	restartTs *common.TsVbuuid) {

	logging.Debugf("Indexer::startBucketStream Stream: %v Bucket: %v RestartTS %v",
		streamId, bucket, restartTs)

	var indexList []common.IndexInst
	var bucketUUIDList []string

	switch streamId {

	case common.MAINT_STREAM:

		for _, indexInst := range idx.indexInstMap {

			if indexInst.Defn.Bucket == bucket {
				switch indexInst.State {
				case common.INDEX_STATE_ACTIVE,
					common.INDEX_STATE_INITIAL:
					if indexInst.Stream == streamId {
						indexList = append(indexList, indexInst)
						bucketUUIDList = append(bucketUUIDList, indexInst.Defn.BucketUUID)
					}
				case common.INDEX_STATE_CATCHUP:
					indexList = append(indexList, indexInst)
					bucketUUIDList = append(bucketUUIDList, indexInst.Defn.BucketUUID)
				}
			}
		}

	case common.INIT_STREAM:

		for _, indexInst := range idx.indexInstMap {
			if indexInst.Defn.Bucket == bucket &&
				indexInst.Stream == streamId {
				switch indexInst.State {
				case common.INDEX_STATE_INITIAL,
					common.INDEX_STATE_CATCHUP:
					indexList = append(indexList, indexInst)
					bucketUUIDList = append(bucketUUIDList, indexInst.Defn.BucketUUID)
				}
			}
		}

	default:
		logging.Errorf("Indexer::startBucketStream \n\t Unsupported StreamId %v", streamId)
		common.CrashOnError(ErrInvalidStream)

	}

	if len(indexList) == 0 {
		logging.Debugf("Indexer::startBucketStream Nothing to Start. Stream: %v Bucket: %v",
			streamId, bucket)
		idx.streamBucketStatus[streamId][bucket] = STREAM_INACTIVE
		return
	}

	respCh := make(MsgChannel)
	stopCh := make(StopChannel)

	cmd := &MsgStreamUpdate{mType: OPEN_STREAM,
		streamId:  streamId,
		bucket:    bucket,
		indexList: indexList,
		restartTs: restartTs,
		buildTs:   idx.bucketBuildTs[bucket],
		respCh:    respCh,
		stopCh:    stopCh}

	//send stream update to timekeeper
	if resp := idx.sendStreamUpdateToWorker(cmd, idx.tkCmdCh,
		"Timekeeper"); resp.GetMsgType() != MSG_SUCCESS {
		respErr := resp.(*MsgError).GetError()
		common.CrashOnError(respErr.cause)
	}

	//send stream update to mutation manager
	if resp := idx.sendStreamUpdateToWorker(cmd, idx.mutMgrCmdCh,
		"MutationMgr"); resp.GetMsgType() != MSG_SUCCESS {
		respErr := resp.(*MsgError).GetError()
		common.CrashOnError(respErr.cause)
	}

	if _, ok := idx.streamBucketRequestStopCh[streamId][bucket]; !ok {
		idx.streamBucketRequestStopCh[streamId] = make(BucketRequestStopCh)
	}
	idx.streamBucketRequestStopCh[streamId][bucket] = stopCh

	clustAddr := idx.config["clusterAddr"].String()
	numVb := idx.config["numVbuckets"].Int()

	reqLock := idx.acquireStreamRequestLock(bucket, streamId)
	go func(reqLock *kvRequest) {
		defer idx.releaseStreamRequestLock(reqLock)
		idx.waitStreamRequestLock(reqLock)
	retryloop:
		for {
			//validate bucket before every try
			if !ValidateBucket(clustAddr, bucket, bucketUUIDList) {
				logging.Errorf("Indexer::startBucketStream \n\tBucket Not Found "+
					"For Stream %v Bucket %v", streamId, bucket)
				idx.internalRecvCh <- &MsgRecovery{mType: INDEXER_BUCKET_NOT_FOUND,
					streamId: streamId,
					bucket:   bucket}
				break retryloop
			}

			idx.sendMsgToKVSender(cmd)

			if resp, ok := <-respCh; ok {

				switch resp.GetMsgType() {

				case MSG_SUCCESS_OPEN_STREAM:

					//once stream request is successful re-calculate the KV timestamp.
					//This makes sure indexer doesn't use a timestamp which can never
					//be caught up to (due to kv rollback).
					//if there is a failover after this, it will be observed as a rollback
					buildTs, err := computeBucketBuildTs(clustAddr, bucket, numVb)
					if err == nil {
						idx.internalRecvCh <- &MsgRecovery{mType: INDEXER_RECOVERY_DONE,
							streamId:  streamId,
							bucket:    bucket,
							buildTs:   buildTs,
							restartTs: restartTs,
							activeTs:  resp.(*MsgSuccessOpenStream).GetActiveTs()}
						break retryloop
					}

				case INDEXER_ROLLBACK:
					logging.Infof("Indexer::startBucketStream \n\tRollback from "+
						"Projector For Stream %v Bucket %v", streamId, bucket)
					rollbackTs := resp.(*MsgRollback).GetRollbackTs()
					idx.internalRecvCh <- &MsgRecovery{mType: INDEXER_INIT_PREP_RECOVERY,
						streamId:  streamId,
						bucket:    bucket,
						restartTs: rollbackTs}
					break retryloop

				default:
					//log and retry for all other responses
					respErr := resp.(*MsgError).GetError()
					logging.Errorf("Indexer::startBucketStream Stream %v Bucket %v "+
						"Error from Projector %v. Retrying.", streamId, bucket,
						respErr.cause)
					time.Sleep(KV_RETRY_INTERVAL * time.Millisecond)
				}
			}
		}
	}(reqLock)
}

func (idx *indexer) processRollback(streamId common.StreamId,
	bucket string, rollbackTs *common.TsVbuuid) (*common.TsVbuuid, error) {

	//send to storage manager to rollback
	msg := &MsgRollback{streamId: streamId,
		bucket:     bucket,
		rollbackTs: rollbackTs}

	idx.storageMgrCmdCh <- msg
	res := <-idx.storageMgrCmdCh

	if res.GetMsgType() != MSG_ERROR {
		rollbackTs := res.(*MsgRollback).GetRollbackTs()
		return rollbackTs, nil
	} else {
		logging.Fatalf("Indexer::processRollback Error during Rollback %v", res)
		respErr := res.(*MsgError).GetError()
		return nil, respErr.cause
	}

}

//helper function to init streamFlush map for all streams
func (idx *indexer) initStreamFlushMap() {

	for i := 0; i < int(common.ALL_STREAMS); i++ {
		idx.streamBucketFlushInProgress[common.StreamId(i)] = make(BucketFlushInProgressMap)
		idx.streamBucketObserveFlushDone[common.StreamId(i)] = make(BucketObserveFlushDoneMap)
	}
}

func (idx *indexer) notifyFlushObserver(msg Message) {

	//if there is any observer for flush, notify
	bucket := msg.(*MsgMutMgrFlushDone).GetBucket()
	streamId := msg.(*MsgMutMgrFlushDone).GetStreamId()

	if notifyCh, ok := idx.streamBucketObserveFlushDone[streamId][bucket]; ok {
		if notifyCh != nil {
			notifyCh <- msg
			//wait for a sync response that cleanup is done.
			//notification is sent one by one as there is no lock
			<-notifyCh
		}
	}
	return
}

func (idx *indexer) processDropAfterFlushDone(indexInst common.IndexInst,
	notifyCh MsgChannel, clientCh MsgChannel) {

	select {
	case <-notifyCh:
		idx.cleanupIndex(indexInst, clientCh)
	}

	streamId := indexInst.Stream
	bucket := indexInst.Defn.Bucket
	idx.streamBucketObserveFlushDone[streamId][bucket] = nil

	//indicate done
	close(notifyCh)
}

func (idx *indexer) checkDuplicateDropRequest(indexInst common.IndexInst,
	respCh MsgChannel) bool {

	//if there is any observer for flush done for this stream/bucket,
	//drop is already in progress
	stream := indexInst.Stream
	bucket := indexInst.Defn.Bucket
	if obs, ok := idx.streamBucketObserveFlushDone[stream][bucket]; ok && obs != nil {

		errStr := "Index Drop Already In Progress."

		logging.Errorf(errStr)
		if respCh != nil {
			respCh <- &MsgError{
				err: Error{code: ERROR_INDEX_DROP_IN_PROGRESS,
					severity: FATAL,
					cause:    errors.New(errStr),
					category: INDEXER}}

		}
		return true
	}
	return false
}

func (idx *indexer) bootstrap(snapshotNotifych chan IndexSnapshot) error {

	idx.genIndexerId()

	//set topic names based on indexer id
	idx.initStreamTopicName()

	//close any old streams with projector
	idx.closeAllStreams()

	//recover indexes from local metadata
	if err := idx.initFromPersistedState(); err != nil {
		return err
	}

	//Start Storage Manager
	var res Message
	idx.storageMgr, res = NewStorageManager(idx.storageMgrCmdCh, idx.wrkrRecvCh,
		idx.indexPartnMap, idx.config, snapshotNotifych)
	if res.GetMsgType() == MSG_ERROR {
		err := res.(*MsgError).GetError()
		logging.Errorf("Indexer::NewIndexer Storage Manager Init Error %v", err)
		return err.cause
	}

	//send updated maps
	msgUpdateIndexInstMap := idx.newIndexInstMsg(idx.indexInstMap)
	msgUpdateIndexPartnMap := &MsgUpdatePartnMap{indexPartnMap: idx.indexPartnMap}

	// Distribute current stats object and index information
	if err := idx.distributeIndexMapsToWorkers(msgUpdateIndexInstMap, msgUpdateIndexPartnMap); err != nil {
		common.CrashOnError(err)
	}

	// ready to process DDL
	msg := &MsgClustMgrUpdate{mType: CLUST_MGR_INDEXER_READY}
	if err := idx.sendMsgToClusterMgr(msg); err != nil {
		return err
	}

	//if there are no indexes, return from here
	if len(idx.indexInstMap) == 0 {
		return nil
	}

	if ok := idx.startStreams(); !ok {
		return errors.New("Unable To Start DCP Streams")
	}

	return nil
}

func (idx *indexer) genIndexerId() {

	if idx.enableManager {

		//try to fetch IndexerId from manager
		idx.clustMgrAgentCmdCh <- &MsgClustMgrLocal{
			mType: CLUST_MGR_GET_LOCAL,
			key:   INDEXER_ID_KEY,
		}

		respMsg := <-idx.clustMgrAgentCmdCh
		resp := respMsg.(*MsgClustMgrLocal)

		val := resp.GetValue()
		err := resp.GetError()

		if err == nil {
			idx.id = val
		} else if strings.Contains(err.Error(), "key not found") {
			//if there is no IndexerId, generate and store in manager

			id, err := common.NewUUID()
			if err == nil {
				idx.id = id.Str()
			} else {
				idx.id = strconv.Itoa(rand.Int())
			}

			idx.clustMgrAgentCmdCh <- &MsgClustMgrLocal{
				mType: CLUST_MGR_SET_LOCAL,
				key:   INDEXER_ID_KEY,
				value: idx.id,
			}

			respMsg := <-idx.clustMgrAgentCmdCh
			resp := respMsg.(*MsgClustMgrLocal)

			errMsg := resp.GetError()
			if errMsg != nil {
				logging.Errorf("Indexer::genIndexerId Unable to set IndexerId In Local"+
					"Meta Storage. Err %v", errMsg)
				common.CrashOnError(errMsg)
			}

		} else {
			logging.Errorf("Indexer::genIndexerId Error Fetching IndexerId From Local"+
				"Meta Storage. Err %v", err)
			common.CrashOnError(err)
		}
	} else {
		//assume 1 without manager
		idx.id = "1"
	}

	logging.Infof("Indexer Id %v", idx.id)

}

func (idx *indexer) initFromPersistedState() error {

	err := idx.recoverIndexInstMap()
	if err != nil {
		logging.Errorf("Indexer::initFromPersistedState Error Recovering IndexInstMap %v", err)
		return err
	}

	logging.Debugf("Indexer::initFromPersistedState Recovered IndexInstMap %v", idx.indexInstMap)

	idx.validateIndexInstMap()

	for _, inst := range idx.indexInstMap {
		if inst.State != common.INDEX_STATE_DELETED {
			idx.stats.AddIndex(inst.InstId, inst.Defn.Bucket, inst.Defn.Name)
		}

		newpc := common.NewKeyPartitionContainer()

		//Add one partition for now
		partnId := common.PartitionId(0)
		addr := net.JoinHostPort("", idx.config["streamMaintPort"].String())
		endpt := []common.Endpoint{common.Endpoint(addr)}
		partnDefn := common.KeyPartitionDefn{Id: partnId,
			Endpts: endpt}
		newpc.AddPartition(partnId, partnDefn)

		inst.Pc = newpc

		//allocate partition/slice
		var partnInstMap PartitionInstMap
		var err error
		if partnInstMap, err = idx.initPartnInstance(inst, nil); err != nil {
			return err
		}

		idx.indexInstMap[inst.InstId] = inst
		idx.indexPartnMap[inst.InstId] = partnInstMap

	}

	return nil

}

func (idx *indexer) recoverIndexInstMap() error {

	if idx.enableManager {
		return idx.recoverInstMapFromManager()
	} else {
		return idx.recoverInstMapFromFile()
	}

}

func (idx *indexer) recoverInstMapFromManager() error {

	idx.clustMgrAgentCmdCh <- &MsgClustMgrTopology{}

	resp := <-idx.clustMgrAgentCmdCh

	switch resp.GetMsgType() {

	case CLUST_MGR_GET_GLOBAL_TOPOLOGY:
		idx.indexInstMap = resp.(*MsgClustMgrTopology).GetInstMap()

	case MSG_ERROR:
		err := resp.(*MsgError).GetError()
		common.CrashOnError(err.cause)

	default:
		common.CrashOnError(errors.New("Unknown Response"))
	}
	return nil
}

func (idx *indexer) recoverInstMapFromFile() error {

	var dbfile *forestdb.File
	var meta *forestdb.KVStore
	var err error

	//read indexer state and local state context
	config := forestdb.DefaultConfig()

	if dbfile, err = forestdb.Open("meta", config); err != nil {
		return err
	}
	defer dbfile.Close()

	kvconfig := forestdb.DefaultKVStoreConfig()
	// Make use of default kvstore provided by forestdb
	if meta, err = dbfile.OpenKVStore("default", kvconfig); err != nil {
		return err
	}

	defer meta.Close()

	//read the instance map
	var instBytes []byte
	instBytes, err = meta.GetKV([]byte(INST_MAP_KEY_NAME))

	//forestdb reports get in a non-existent key as an
	//error, skip that
	if err != nil && err != forestdb.RESULT_KEY_NOT_FOUND {
		return err
	}

	//if there is no instance map available, proceed with
	//normal init
	if len(instBytes) == 0 {
		return nil
	}

	decBuf := bytes.NewBuffer(instBytes)
	dec := gob.NewDecoder(decBuf)
	err = dec.Decode(&idx.indexInstMap)

	if err != nil {
		logging.Errorf("Indexer::recoverInstMapFromFile Decode Error %v", err)
		return err
	}
	return nil
}

func (idx *indexer) validateIndexInstMap() {

	bucketUUIDMap := make(map[string]bool)
	bucketValid := make(map[string]bool)

	for instId, index := range idx.indexInstMap {

		//only indexes in created, initial, catchup, active state
		//are valid for recovery
		if !isValidRecoveryState(index.State) {
			logging.Debugf("Indexer::validateIndexInstMap \n\t State %v Not Recoverable. "+
				"Not Recovering Index %v", index.State, index)
			delete(idx.indexInstMap, instId)
			continue
		}

		//if bucket doesn't exist, cleanup
		bucketUUID := index.Defn.Bucket + "::" + index.Defn.BucketUUID
		if _, ok := bucketUUIDMap[bucketUUID]; !ok {

			bucket := index.Defn.Bucket
			bucketUUIDValid := ValidateBucket(idx.config["clusterAddr"].String(), bucket, []string{index.Defn.BucketUUID})
			bucketUUIDMap[bucketUUID] = bucketUUIDValid

			if _, ok := bucketValid[bucket]; ok {
				bucketValid[bucket] = bucketValid[bucket] && bucketUUIDValid
			} else {
				bucketValid[bucket] = bucketUUIDValid
			}

			//also set the buildTs for initial state index.
			//TODO buildTs to be part of index instance
			if bucketUUIDValid {
				cluster := idx.config["clusterAddr"].String()
				numVbuckets := idx.config["numVbuckets"].Int()
				buildTs, err := GetCurrentKVTs(cluster, "default", bucket, numVbuckets)
				if err != nil {
					common.CrashOnError(err)
				} else {
					idx.bucketBuildTs[bucket] = buildTs
				}
			}
		}
	}

	// handle bucket that fails validation
	for bucket, valid := range bucketValid {
		if !valid {
			instList := idx.deleteIndexInstOnDeletedBucket(bucket, common.NIL_STREAM)
			for _, instId := range instList {
				index := idx.indexInstMap[instId]
				logging.Errorf("Indexer::validateIndexInstMap \n\t Bucket %v Not Found."+
					"Not Recovering Index %v", bucket, index)
				delete(idx.indexInstMap, instId)
			}
		}
	}

	idx.checkMissingMaintBucket()

}

//On recovery, deleted indexes are ignored. There can be
//a case where the last maint stream index was dropped and
//indexer crashes while there is an index in Init stream.
//Such indexes need to be moved to Maint Stream.
func (idx *indexer) checkMissingMaintBucket() {

	missingBucket := make(map[string]bool)

	//get all unique buckets in init stream
	for _, index := range idx.indexInstMap {
		if index.Stream == common.INIT_STREAM {
			missingBucket[index.Defn.Bucket] = true
		}
	}

	//remove those present in maint stream
	for _, index := range idx.indexInstMap {
		if index.Stream == common.MAINT_STREAM {
			if _, ok := missingBucket[index.Defn.Bucket]; ok {
				delete(missingBucket, index.Defn.Bucket)
			}
		}
	}

	//move indexes of these buckets to Maint Stream
	if len(missingBucket) > 0 {
		var updatedList []common.IndexInstId
		for bucket, _ := range missingBucket {
			//for all indexes for this bucket
			for instId, index := range idx.indexInstMap {
				if index.Defn.Bucket == bucket {
					//state is set to Initial, no catchup in Maint
					index.State = common.INDEX_STATE_INITIAL
					index.Stream = common.MAINT_STREAM
					idx.indexInstMap[instId] = index
					updatedList = append(updatedList, instId)
				}
			}
		}

		if idx.enableManager {
			if err := idx.updateMetaInfoForIndexList(updatedList,
				true, true, false, false); err != nil {
				common.CrashOnError(err)
			}
		}
	}
}

func isValidRecoveryState(state common.IndexState) bool {

	switch state {

	case common.INDEX_STATE_CREATED,
		common.INDEX_STATE_INITIAL,
		common.INDEX_STATE_CATCHUP,
		common.INDEX_STATE_ACTIVE:
		return true

	default:
		return false

	}

}

func (idx *indexer) startStreams() bool {

	//Start MAINT_STREAM
	restartTs := idx.makeRestartTs(common.MAINT_STREAM)

	idx.streamBucketStatus[common.MAINT_STREAM] = make(BucketStatus)
	for bucket, ts := range restartTs {
		idx.startBucketStream(common.MAINT_STREAM, bucket, ts)
		idx.streamBucketStatus[common.MAINT_STREAM][bucket] = STREAM_ACTIVE
	}

	//Start INIT_STREAM
	restartTs = idx.makeRestartTs(common.INIT_STREAM)

	idx.streamBucketStatus[common.INIT_STREAM] = make(BucketStatus)
	for bucket, ts := range restartTs {
		idx.startBucketStream(common.INIT_STREAM, bucket, ts)
		idx.streamBucketStatus[common.INIT_STREAM][bucket] = STREAM_ACTIVE
	}

	return true

}

func (idx *indexer) makeRestartTs(streamId common.StreamId) map[string]*common.TsVbuuid {

	restartTs := make(map[string]*common.TsVbuuid)

	for idxInstId, partnMap := range idx.indexPartnMap {
		idxInst := idx.indexInstMap[idxInstId]

		if idxInst.Stream == streamId {

			//there is only one partition for now
			partnInst := partnMap[0]
			sc := partnInst.Sc

			//there is only one slice for now
			slice := sc.GetSliceById(0)

			infos, err := slice.GetSnapshots()
			// TODO: Proper error handling if possible
			if err != nil {
				panic("Unable read snapinfo -" + err.Error())
			}

			s := NewSnapshotInfoContainer(infos)
			latestSnapInfo := s.GetLatest()

			//There may not be a valid snapshot info if no flush
			//happened for this index
			if latestSnapInfo != nil {
				ts := latestSnapInfo.Timestamp()
				if oldTs, ok := restartTs[idxInst.Defn.Bucket]; ok {
					if !ts.AsRecent(oldTs) {
						restartTs[idxInst.Defn.Bucket] = ts
					}
				} else {
					restartTs[idxInst.Defn.Bucket] = ts
				}
			} else {
				//set restartTs to nil for this bucket
				if _, ok := restartTs[idxInst.Defn.Bucket]; !ok {
					restartTs[idxInst.Defn.Bucket] = nil
				}
			}
		}
	}
	return restartTs
}

func (idx *indexer) closeAllStreams() {

	respCh := make(MsgChannel)

	for i := 0; i < int(common.ALL_STREAMS); i++ {

		//skip for nil and catchup stream
		if i == int(common.NIL_STREAM) ||
			i == int(common.CATCHUP_STREAM) {
			continue
		}

		cmd := &MsgStreamUpdate{mType: CLOSE_STREAM,
			streamId: common.StreamId(i),
			respCh:   respCh,
		}

	retryloop:
		for {
			idx.sendMsgToKVSender(cmd)

			if resp, ok := <-respCh; ok {

				switch resp.GetMsgType() {

				case MSG_SUCCESS:
					break retryloop

				default:
					//log and retry for all other responses
					respErr := resp.(*MsgError).GetError()
					logging.Errorf("Indexer::closeAllStreams Stream %v "+
						"Error from Projector %v. Retrying.", common.StreamId(i), respErr.cause)
					time.Sleep(KV_RETRY_INTERVAL * time.Millisecond)
				}
			}
		}
	}
}

func (idx *indexer) checkStreamRequestPending(streamId common.StreamId, bucket string) bool {

	if bmap, ok := idx.streamBucketRequestStopCh[streamId]; ok {
		if stopCh, ok := bmap[bucket]; ok {
			if stopCh != nil {
				return true
			}
		}
	}

	return false
}

func (idx *indexer) updateMetaInfoForBucket(bucket string,
	updateState bool, updateStream bool, updateError bool) error {

	var instIdList []common.IndexInstId
	for _, inst := range idx.indexInstMap {
		if inst.Defn.Bucket == bucket {
			instIdList = append(instIdList, inst.InstId)
		}
	}

	if len(instIdList) != 0 {
		return idx.updateMetaInfoForIndexList(instIdList, updateState,
			updateStream, updateError, false)
	} else {
		return nil
	}

}

func (idx *indexer) updateMetaInfoForIndexList(instIdList []common.IndexInstId,
	updateState bool, updateStream bool, updateError bool, updateBuildTs bool) error {

	var indexList []common.IndexInst
	for _, instId := range instIdList {
		indexList = append(indexList, idx.indexInstMap[instId])
	}

	updatedFields := MetaUpdateFields{
		state:   updateState,
		stream:  updateStream,
		err:     updateError,
		buildTs: updateBuildTs,
	}

	msg := &MsgClustMgrUpdate{
		mType:         CLUST_MGR_UPDATE_TOPOLOGY_FOR_INDEX,
		indexList:     indexList,
		updatedFields: updatedFields}

	return idx.sendMsgToClusterMgr(msg)

}

func (idx *indexer) updateMetaInfoForDeleteBucket(bucket string, streamId common.StreamId) error {

	msg := &MsgClustMgrUpdate{mType: CLUST_MGR_DEL_BUCKET, bucket: bucket, streamId: streamId}
	return idx.sendMsgToClusterMgr(msg)
}

func (idx *indexer) sendMsgToClusterMgr(msg Message) error {

	idx.clustMgrAgentCmdCh <- msg

	if res, ok := <-idx.clustMgrAgentCmdCh; ok {

		switch res.GetMsgType() {

		case MSG_SUCCESS:
			return nil

		case MSG_ERROR:
			logging.Debugf("Indexer::sendMsgToClusterMgr Error "+
				"from Cluster Manager %v", res)
			err := res.(*MsgError).GetError()
			return err.cause

		default:
			logging.Debugf("Indexer::sendMsgToClusterMgr Unknown Response "+
				"from Cluster Manager %v", res)
			common.CrashOnError(errors.New("Unknown Response"))

		}

	} else {

		logging.Debugf("clustMgrAgent::sendMsgToClusterMgr Unexpected Channel Close " +
			"from Cluster Manager")
		common.CrashOnError(errors.New("Unknown Response"))

	}

	return nil
}

func (idx *indexer) bulkUpdateError(instIdList []common.IndexInstId,
	errStr string) {

	for _, instId := range instIdList {
		idxInst := idx.indexInstMap[instId]
		idxInst.Error = errStr
		idx.indexInstMap[instId] = idxInst
	}

}

func (idx *indexer) bulkUpdateState(instIdList []common.IndexInstId,
	state common.IndexState) {

	for _, instId := range instIdList {
		idxInst := idx.indexInstMap[instId]
		idxInst.State = state
		idx.indexInstMap[instId] = idxInst
	}
}

func (idx *indexer) bulkUpdateStream(instIdList []common.IndexInstId,
	stream common.StreamId) {

	for _, instId := range instIdList {
		idxInst := idx.indexInstMap[instId]
		idxInst.Stream = stream
		idx.indexInstMap[instId] = idxInst
	}
}

func (idx *indexer) bulkUpdateBuildTs(instIdList []common.IndexInstId,
	buildTs Timestamp) {

	for _, instId := range instIdList {
		idxInst := idx.indexInstMap[instId]
		buildTs := make([]uint64, len(buildTs))
		for i, ts := range buildTs {
			buildTs[i] = uint64(ts)
		}
		idxInst.BuildTs = buildTs
		idx.indexInstMap[instId] = idxInst
	}
}

func (idx *indexer) checkBucketInRecovery(bucket string,
	instIdList []common.IndexInstId, clientCh MsgChannel, errMap map[common.IndexInstId]error) bool {

	if idx.streamBucketStatus[common.INIT_STREAM][bucket] == STREAM_RECOVERY ||
		idx.streamBucketStatus[common.INIT_STREAM][bucket] == STREAM_PREPARE_RECOVERY ||
		idx.streamBucketStatus[common.MAINT_STREAM][bucket] == STREAM_RECOVERY ||
		idx.streamBucketStatus[common.MAINT_STREAM][bucket] == STREAM_PREPARE_RECOVERY {

		if idx.enableManager {
			errStr := fmt.Sprintf("Bucket %v In Recovery", bucket)
			idx.bulkUpdateError(instIdList, errStr)
			for _, instId := range instIdList {
				errMap[instId] = errors.New(errStr)
			}
		} else if clientCh != nil {
			clientCh <- &MsgError{
				err: Error{code: ERROR_INDEXER_IN_RECOVERY,
					severity: FATAL,
					cause:    ErrIndexerInRecovery,
					category: INDEXER}}
		}
		return true
	}
	return false
}

func (idx *indexer) checkValidIndexInst(bucket string,
	instIdList []common.IndexInstId, clientCh MsgChannel, errMap map[common.IndexInstId]error) bool {

	//validate instance list
	for _, instId := range instIdList {
		if _, ok := idx.indexInstMap[instId]; !ok {
			if idx.enableManager {
				errStr := fmt.Sprintf("Unknown Index Instance %v In Build Request", instId)
				idx.bulkUpdateError(instIdList, errStr)
				for _, instId := range instIdList {
					errMap[instId] = errors.New(errStr)
				}
			} else if clientCh != nil {
				clientCh <- &MsgError{
					err: Error{code: ERROR_INDEXER_UNKNOWN_INDEX,
						severity: FATAL,
						cause:    ErrIndexNotFound,
						category: INDEXER}}
			}
			return false
		}
	}

	return true
}

func (idx *indexer) groupIndexListByBucket(instIdList []common.IndexInstId) map[string][]common.IndexInstId {

	bucketInstList := make(map[string][]common.IndexInstId)
	for _, instId := range instIdList {
		indexInst := idx.indexInstMap[instId]
		if instList, ok := bucketInstList[indexInst.Defn.Bucket]; ok {
			instList = append(instList, indexInst.InstId)
			bucketInstList[indexInst.Defn.Bucket] = instList
		} else {
			var newInstList []common.IndexInstId
			newInstList = append(newInstList, indexInst.InstId)
			bucketInstList[indexInst.Defn.Bucket] = newInstList
		}
	}
	return bucketInstList

}

func (idx *indexer) checkBucketExists(bucket string,
	instIdList []common.IndexInstId, clientCh MsgChannel, errMap map[common.IndexInstId]error) bool {

	var bucketUUIDList []string
	for _, instId := range instIdList {
		indexInst := idx.indexInstMap[instId]
		if indexInst.Defn.Bucket == bucket {
			bucketUUIDList = append(bucketUUIDList, indexInst.Defn.BucketUUID)
		}
	}

	if !ValidateBucket(idx.config["clusterAddr"].String(), bucket, bucketUUIDList) {
		if idx.enableManager {
			errStr := fmt.Sprintf("Unknown Bucket %v In Build Request", bucket)
			idx.bulkUpdateError(instIdList, errStr)
			for _, instId := range instIdList {
				errMap[instId] = errors.New(errStr)
			}
		} else if clientCh != nil {
			clientCh <- &MsgError{
				err: Error{code: ERROR_INDEXER_UNKNOWN_BUCKET,
					severity: FATAL,
					cause:    ErrUnknownBucket,
					category: INDEXER}}
		}
		return false
	}
	return true
}

func (idx *indexer) handleStats(cmd Message) {
	req := cmd.(*MsgStatsRequest)
	replych := req.GetReplyChannel()
	idx.stats.memoryUsed.Set(idx.memoryUsed())
	replych <- true
}

func (idx *indexer) handleResetStats() {
	idx.stats.Reset()
	msgUpdateIndexInstMap := idx.newIndexInstMsg(idx.indexInstMap)
	if err := idx.distributeIndexMapsToWorkers(msgUpdateIndexInstMap, nil); err != nil {
		common.CrashOnError(err)
	}
}

func (idx *indexer) memoryUsed() int64 {
	return int64(forestdb.BufferCacheUsed())
}

func NewSlice(id SliceId, indInst *common.IndexInst,
	conf common.Config, stats *IndexerStats) (slice Slice, err error) {

	if indInst.Defn.Using == common.MemDB {
		slice, err = NewMemDBSlice(id, indInst.Defn.DefnId, indInst.InstId, indInst.Defn.IsPrimary, conf)
	} else {
		// Default storage is forestdb
		storage_dir := conf["storage_dir"].String()
		os.Mkdir(storage_dir, 0755)
		if _, e := os.Stat(storage_dir); e != nil {
			common.CrashOnError(e)
		}
		path := filepath.Join(storage_dir, IndexPath(indInst, id))
		slice, err = NewForestDBSlice(path, id, indInst.Defn.DefnId, indInst.InstId, indInst.Defn.IsPrimary, conf, stats.indexes[indInst.InstId])
	}

	return
}

func (idx *indexer) setProfilerOptions(config common.Config) {

	logging.Infof("New Settings Received ")

	// CPU-profiling
	cpuProfile, ok := config["settings.cpuProfile"]
	if ok && cpuProfile.Bool() && idx.cpuProfFd == nil {
		cpuProfFname, ok := config["settings.cpuProfFname"]
		if ok {
			fname := cpuProfFname.String()
			logging.Infof("Indexer:: cpu profiling => %q\n", fname)
			idx.cpuProfFd = startCPUProfile(fname)

		} else {
			logging.Errorf("Indexer:: Missing cpu-profile o/p filename\n")
		}

	} else if ok && !cpuProfile.Bool() {
		if idx.cpuProfFd != nil {
			pprof.StopCPUProfile()
			logging.Infof("Indexer:: cpu profiling stopped\n")
		}
		idx.cpuProfFd = nil

	}

	// MEM-profiling
	memProfile, ok := config["settings.memProfile"]
	if ok && memProfile.Bool() {
		memProfFname, ok := config["settings.memProfFname"]
		if ok {
			fname := memProfFname.String()
			if dumpMemProfile(fname) {
				logging.Infof("Indexer:: mem profile => %q\n", fname)
			}
		} else {
			logging.Errorf("Indexer:: Missing mem-profile o/p filename\n")
		}
	}
}

func (idx *indexer) getIndexInstForBucket(bucket string) ([]common.IndexInstId, error) {

	idx.clustMgrAgentCmdCh <- &MsgClustMgrTopology{}
	resp := <-idx.clustMgrAgentCmdCh

	var result []common.IndexInstId = nil

	switch resp.GetMsgType() {
	case CLUST_MGR_GET_GLOBAL_TOPOLOGY:
		instMap := resp.(*MsgClustMgrTopology).GetInstMap()
		for id, inst := range instMap {
			if inst.Defn.Bucket == bucket {
				result = append(result, id)
			}
		}
	default:
		return nil, errors.New("Fail to read Metadata")
	}

	return result, nil
}

func (idx *indexer) deleteIndexInstOnDeletedBucket(bucket string, streamId common.StreamId) []common.IndexInstId {

	var instIdList []common.IndexInstId = nil

	if idx.enableManager {
		if err := idx.updateMetaInfoForDeleteBucket(bucket, streamId); err != nil {
			common.CrashOnError(err)
		}
	}

	// Only mark index inst as DELETED if it is actually got deleted in metadata.
	for _, index := range idx.indexInstMap {
		if index.Defn.Bucket == bucket &&
			(streamId == common.NIL_STREAM || index.Stream == streamId) {

			instIdList = append(instIdList, index.InstId)
			idx.stats.RemoveIndex(index.InstId)
		}
	}

	return instIdList
}

// start cpu profiling.
func startCPUProfile(filename string) *os.File {
	if filename == "" {
		fmsg := "Indexer:: empty cpu profile filename\n"
		logging.Errorf(fmsg, filename)
		return nil
	}
	fd, err := os.Create(filename)
	if err != nil {
		logging.Errorf("Indexer:: unable to create %q: %v\n", filename, err)
	}
	pprof.StartCPUProfile(fd)
	return fd
}

func dumpMemProfile(filename string) bool {
	if filename == "" {
		fmsg := "Indexer:: empty mem profile filename\n"
		logging.Errorf(fmsg, filename)
		return false
	}
	fd, err := os.Create(filename)
	if err != nil {
		logging.Errorf("Indexer:: unable to create %q: %v\n", filename, err)
		return false
	}
	pprof.WriteHeapProfile(fd)
	defer fd.Close()
	return true
}

//calculates buildTs for bucket. This is a blocking call
//which will keep trying till success as indexer cannot work
//without a buildts.
func computeBucketBuildTs(clustAddr string, bucket string, numVb int) (buildTs Timestamp, err error) {
kvtsloop:
	for {
		buildTs, err = GetCurrentKVTs(clustAddr, "default", bucket, numVb)
		if err != nil {
			logging.Errorf("Indexer::computeBucketBuildTs Error Fetching BuildTs %v", err)
			time.Sleep(KV_RETRY_INTERVAL * time.Millisecond)
		} else {
			break kvtsloop
		}
	}
	return
}

func (idx *indexer) updateSliceWithConfig(config common.Config) {

	//for every index managed by this indexer
	for _, partnMap := range idx.indexPartnMap {

		//for all partitions managed by this indexer
		for _, partnInst := range partnMap {

			sc := partnInst.Sc

			//update config for all the slices
			for _, slice := range sc.GetAllSlices() {
				slice.UpdateConfig(config)
			}
		}
	}

}
