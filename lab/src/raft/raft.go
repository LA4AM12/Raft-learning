package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"6.824/labgob"
	"6.824/labrpc"
)

// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.RWMutex        // Lock to protect shared access to this peer's state
	syncCond  []*sync.Cond        // signal replicator goroutine to batch replicating entries
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	state          State
	applyCh        chan ApplyMsg
	electionTimer  *time.Timer
	heartbeatTimer *time.Timer

	// Persistent state on all servers
	currentTerm int
	votedFor    int
	logs        []Entry

	// Volatile state on all servers
	commitIndex int
	lastApplied int

	// Volatile state on leaders (Reinitialized after election)
	nextIndex  []int
	matchIndex []int
}

type Entry struct {
	Term    int
	Index   int
	Command interface{}
}

type State int

const (
	Leader = iota
	Follower
	Candidate
)

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (2A).
	rf.mu.RLock()
	defer rf.mu.RUnlock()
	term = rf.currentTerm
	if rf.state == Leader {
		isleader = true
	}
	return term, isleader
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
func (rf *Raft) persist() {
	// Your code here (2C).
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.logs)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int
	var votedFor int
	var logs []Entry
	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&logs) != nil {
		DPrintf("server %d decode error", rf.me)
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.logs = logs
	}
}

// A service wants to switch to snapshot.  Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {

	// Your code here (2D).

	return true
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

// AppendEntries RPC arguments structure.
type AppendEntriesArgs struct {
	// Your data here (2A, 2B).
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	LeaderCommit int
	Entries      []Entry
}

// AppendEntries RPC reply structure.
type AppendEntriesReply struct {
	// Your data here (2A).
	Term    int
	Success bool
	XTerm   int
	XIndex  int
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term < rf.currentTerm {
		reply.Term, reply.VoteGranted = rf.currentTerm, false
		return
	}

	if args.Term > rf.currentTerm {
		rf.convertToFollower(args.Term)
	}

	// up to now, rf.currentTerm must eq args.term
	lastLogEntry := rf.logs[len(rf.logs)-1]
	upToDate := args.LastLogTerm > lastLogEntry.Term || args.LastLogTerm == lastLogEntry.Term && args.LastLogIndex >= lastLogEntry.Index
	if !upToDate || rf.votedFor != -1 && rf.votedFor != args.CandidateId {
		if upToDate {
			DPrintf("[Reject Vote] [%v] to [%v] term %v: already voted to %v", rf.me, args.CandidateId, rf.currentTerm, rf.votedFor)
		} else {
			DPrintf("[Reject Vote] [%v] to [%v] term %v: no up to date", rf.me, args.CandidateId, rf.currentTerm)
		}
		reply.Term, reply.VoteGranted = rf.currentTerm, false
		return
	}

	// reset timer while granting vote
	rf.electionTimer.Reset(RandomizedElectionTimeout())
	rf.votedFor = args.CandidateId
	rf.persist()
	reply.Term, reply.VoteGranted = rf.currentTerm, true
}

// AppendEntries RPC handler.
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term < rf.currentTerm {
		reply.Term, reply.Success = rf.currentTerm, false
		return
	}
	if args.Term > rf.currentTerm || rf.state == Candidate {
		rf.convertToFollower(args.Term)
	}
	rf.electionTimer.Reset(RandomizedElectionTimeout())
	// Reply false if log doesn't contain an entry at prevLogIndex
	lastLogIndex := rf.logs[len(rf.logs)-1].Index
	if args.PrevLogIndex > lastLogIndex {
		DPrintf("[AppendEntries] server %v doesn't contain an entry at prevLogIndex %v", rf.me, args.PrevLogIndex)
		reply.Term, reply.Success, reply.XTerm, reply.XIndex = rf.currentTerm, false, -1, lastLogIndex+1
		return
	}
	// If an existing entry conflicts with a new one (same index but different terms)
	if rf.logs[args.PrevLogIndex].Term != args.PrevLogTerm {
		// Optimize, maybe
		conflictTerm, conflictIndex := rf.logs[args.PrevLogIndex].Term, args.PrevLogIndex
		DPrintf("[AppendEntries] server %v an existing entry(term %v) conflicts with a new one(term %v) at index %v", rf.me, conflictTerm, args.Term, conflictIndex)
		for conflictIndex > 1 && rf.logs[conflictIndex-1].Term == conflictTerm {
			conflictIndex--
		}
		reply.Term, reply.Success, reply.XTerm, reply.XIndex = rf.currentTerm, false, conflictTerm, conflictIndex

		// delete the existing entry and all that follow it
		rf.logs = rf.logs[:args.PrevLogIndex]
		rf.persist()
		return
	}
	// Append any new entries not already in the log
	for i, e := range args.Entries {
		if e.Index > lastLogIndex || e.Term != rf.logs[e.Index].Term {
			rf.logs = append(rf.logs[:e.Index], args.Entries[i:]...)
			rf.persist()
			DPrintf("[AppendEntries] server %v append new entries: %v at idx %v", rf.me, args.Entries[i:], e.Index)
			break
		}
	}

	if args.LeaderCommit > rf.commitIndex {
		// set commitIndex = min(leaderCommit, index of last new entry)
		rf.commitIndex = Min(args.LeaderCommit, args.PrevLogIndex+len(args.Entries))
		rf.applyCommand()
	}

	reply.Term, reply.Success = rf.currentTerm, true
}

// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	if ok {
		DPrintf("RequestVote [%v] to [%v]: {args %v} {reply %v}", args.CandidateId, server, args, reply)
	}
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	if ok {
		DPrintf("AppendEntries [%v] to [%v]: {args %v} {reply %v}", args.LeaderId, server, args, reply)
	}
	return ok
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	// Your code here (2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.state != Leader {
		return -1, -1, false
	}
	entry := Entry{
		Term:    rf.currentTerm,
		Index:   len(rf.logs),
		Command: command,
	}
	rf.logs = append(rf.logs, entry)
	rf.persist()
	rf.matchIndex[rf.me] = entry.Index
	rf.nextIndex[rf.me] = rf.matchIndex[rf.me] + 1
	for peer := range rf.peers {
		if peer != rf.me {
			rf.syncCond[peer].Signal()
		}
	}
	DPrintf("[Start] server %v append entry %v", rf.me, entry)
	return entry.Index, entry.Term, true
}

func (rf *Raft) Replicator(peer int) {
	rf.syncCond[peer].L.Lock()
	defer rf.syncCond[peer].L.Unlock()
	for !rf.killed() {
		for !rf.needSync(peer) {
			rf.syncCond[peer].Wait()
		}
		rf.Sync(peer)
	}
}

func (rf *Raft) needSync(peer int) bool {
	rf.mu.RLock()
	defer rf.mu.RUnlock()
	return rf.state == Leader && rf.matchIndex[peer] < rf.logs[len(rf.logs)-1].Index
}

func (rf *Raft) Sync(peer int) {
	rf.mu.RLock()
	args := AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: rf.nextIndex[peer] - 1,
		PrevLogTerm:  rf.logs[rf.nextIndex[peer]-1].Term,
		LeaderCommit: rf.commitIndex,
		Entries:      rf.logs[rf.nextIndex[peer]:],
	}
	rf.mu.RUnlock()

	if len(args.Entries) == 0 {
		DPrintf("[Heartbeat] server %v to %v", rf.me, peer)
	}
	reply := AppendEntriesReply{}
	ok := rf.sendAppendEntries(peer, &args, &reply)
	if !ok {
		return
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	// stale reply
	if rf.state != Leader || args.Term != rf.currentTerm {
		return
	}

	// handle reply
	if reply.Term > rf.currentTerm {
		rf.convertToFollower(reply.Term)
		return
	}
	if reply.Success {
		rf.matchIndex[peer] = Max(rf.matchIndex[peer], args.PrevLogIndex+len(args.Entries))
		rf.nextIndex[peer] = rf.matchIndex[peer] + 1
		if rf.canCommit(rf.matchIndex[peer]) {
			rf.commitIndex = rf.matchIndex[peer]
			DPrintf("{server %v} commit log[%v]", rf.me, rf.commitIndex)
			rf.applyCommand()
		}
	} else {
		if reply.XTerm == -1 {
			rf.nextIndex[peer] = reply.XIndex
		} else {
			// we doubt this optimization is necessary
			next := rf.nextIndex[peer] - 1
			for next > 1 && rf.logs[next-1].Term > reply.XTerm {
				next--
			}
			// leader find XTerm
			if rf.logs[next-1].Term == reply.XTerm {
				rf.nextIndex[peer] = next
			} else {
				rf.nextIndex[peer] = reply.XIndex
			}
		}
		rf.syncCond[peer].Signal()
	}
}

// locked outside
func (rf *Raft) canCommit(matchIndex int) bool {
	if matchIndex <= rf.commitIndex {
		//DPrintf("[canCommit] server %v log %v has already committed", rf.me, matchIndex)
		return false
	}

	if rf.logs[matchIndex].Term != rf.currentTerm {
		//DPrintf("[canCommit] server %v log %v term %v doesn't match current term %v", rf.me, matchIndex, rf.log[matchIndex].Term, rf.currentTerm)
		return false
	}

	var quorum int
	for peer := range rf.peers {
		if rf.matchIndex[peer] >= matchIndex {
			quorum++
			if quorum > len(rf.peers)/2 {
				return true
			}
		}
	}
	//DPrintf("[canCommit] server %v log %v quorum %v: not majority", rf.me, matchIndex, quorum)
	return false
}

// locked outside
func (rf *Raft) applyCommand() {
	for rf.lastApplied < rf.commitIndex {
		rf.lastApplied++
		msg := ApplyMsg{
			CommandValid: true,
			Command:      rf.logs[rf.lastApplied].Command,
			CommandIndex: rf.logs[rf.lastApplied].Index,
		}
		rf.applyCh <- msg
		DPrintf("{server %v} applied log[%v]", rf.me, rf.lastApplied)
	}
}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func (rf *Raft) ticker() {
	for !rf.killed() {
		select {
		case <-rf.electionTimer.C:
			rf.startElection()
		case <-rf.heartbeatTimer.C:
			if _, leader := rf.GetState(); leader {
				rf.broadcastHeartbeat()
			}
		}
	}
}

func StableHeartbeatTimeout() time.Duration {
	return 100 * time.Millisecond
}

func RandomizedElectionTimeout() time.Duration {
	rand.Seed(time.Now().UnixNano())
	d := rand.Intn(150) + 250
	return time.Duration(d) * time.Millisecond
}

func (rf *Raft) broadcastHeartbeat() {
	rf.heartbeatTimer.Reset(StableHeartbeatTimeout())
	for peer := range rf.peers {
		if peer != rf.me {
			go rf.Sync(peer)
		}
	}
}

func (rf *Raft) convertToLeader() {
	DPrintf("server %v convert to leader in term %v", rf.me, rf.currentTerm)
	rf.state = Leader
	lastLogEntry := rf.logs[len(rf.logs)-1]
	for peer := range rf.peers {
		rf.matchIndex[peer] = 0
		rf.nextIndex[peer] = lastLogEntry.Index + 1
	}
	rf.electionTimer.Stop()
	rf.broadcastHeartbeat()
}

func (rf *Raft) convertToFollower(term int) {
	if rf.state == Leader {
		rf.electionTimer.Reset(RandomizedElectionTimeout())
	}
	DPrintf("{server %v} convert to follower with term %v", rf.me, term)
	rf.currentTerm, rf.votedFor, rf.state = term, -1, Follower
	rf.persist()
}

func (rf *Raft) startElection() {
	rf.mu.Lock()
	rf.currentTerm++
	rf.state = Candidate
	rf.votedFor = rf.me
	rf.persist()
	rf.electionTimer.Reset(RandomizedElectionTimeout())
	lastLogEntry := rf.logs[len(rf.logs)-1]
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: lastLogEntry.Index,
		LastLogTerm:  lastLogEntry.Term,
	}
	rf.mu.Unlock()

	DPrintf("server %v starts election in term %v", args.CandidateId, args.Term)

	grantedVotes := 1
	for peer := range rf.peers {
		if peer != rf.me {
			go func(peer int) {
				reply := RequestVoteReply{}
				ok := rf.sendRequestVote(peer, &args, &reply)
				if !ok {
					return
				}
				rf.mu.Lock()
				defer rf.mu.Unlock()
				// check current state
				if rf.state != Candidate || args.Term != rf.currentTerm {
					return
				}

				if rf.currentTerm < reply.Term {
					rf.convertToFollower(reply.Term)
				}

				if reply.VoteGranted {
					grantedVotes++
					if grantedVotes > len(rf.peers)/2 {
						rf.convertToLeader()
					}
				}
			}(peer)
		}
	}
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{
		syncCond:       make([]*sync.Cond, len(peers)),
		peers:          peers,
		persister:      persister,
		me:             me,
		state:          Follower,
		applyCh:        applyCh,
		electionTimer:  time.NewTimer(RandomizedElectionTimeout()),
		heartbeatTimer: time.NewTimer(StableHeartbeatTimeout()),
		votedFor:       -1,
		logs:           make([]Entry, 1),
		nextIndex:      make([]int, len(peers)),
		matchIndex:     make([]int, len(peers)),
	}

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	for peer := range peers {
		if peer != me {
			rf.syncCond[peer] = sync.NewCond(&sync.Mutex{})
			go rf.Replicator(peer)
		}
	}
	go rf.ticker()
	return rf
}
