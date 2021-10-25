package parallelsearch

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/gammazero/workerpool"
)

////////////////////////////////////////////////////////////////////////////////

// Searchable is a "node" in a search tree in which we are looking for something.
type Searchable interface {
	Search(onNext func(Searchable))
	IsFound() bool
	Score() int
}

////////////////////////////////////////////////////////////////////////////////

// ParallelSearch implements a breadth-first search of a tree of searchable "nodes"
// This is done in parallel using a FIFO worker pool.
type ParallelSearch struct {
	workerPool  *workerpool.WorkerPool
	depthLimit  int
	searchLimit int
	waiters     []*sync.WaitGroup
	searched    []*uint64
	found       chan Searchable
}

// New creates a new parallel search.  The poolSize determines the number of simultaneous
// workers looking for search results.  The depthLimit restricts how deep we allow the
// breadth-first search to proceed.  The searchLimit determines how many results we are
// looking for before stopping.
func New(poolSize int, depthLimit int, searchLimit int) *ParallelSearch {
	ps := &ParallelSearch{}
	ps.workerPool = workerpool.New(poolSize)
	ps.depthLimit = depthLimit
	ps.searchLimit = searchLimit
	ps.waiters = make([]*sync.WaitGroup, depthLimit+1) // Allow for depth of 0 in addition to other depths
	for depth := range ps.waiters {
		ps.waiters[depth] = &sync.WaitGroup{}
	}
	ps.searched = make([]*uint64, depthLimit+1)
	for depth := range ps.searched {
		d := uint64(0)
		ps.searched[depth] = &d
	}
	ps.found = make(chan Searchable, searchLimit)
	return ps
}

// Start will initiate a new search with the given starting "node" or "nodes".  It will
// announce the completion of each depth/layer as it proceeds.  NOTE: This method should
// only be called once to avoid duplicate depth announcement.
func (self *ParallelSearch) Start(searchables ...Searchable) {
	for _, searchable := range searchables {
		self.asyncSearch(searchable, 0)
	}
	go self.announceDepthCompletion()
}

// WaitForFound will wait until either we have found searchLimit results or we have reached
// the depthLimit with no more "nodes" to consider.  Either way the results found (if any)
// will be sorted by score and returned.
func (self *ParallelSearch) WaitForFound() []Searchable {
	found := []Searchable{}
	for searchable := range self.found {
		found = append(found, searchable)
		if len(found) >= self.searchLimit {
			break
		}
	}
	// Sort results by "Score"
	sort.Slice(found, func(i, j int) bool {
		return found[i].Score() > found[j].Score()
	})
	return found
}

func (self *ParallelSearch) asyncSearch(searchable Searchable, depth int) {
	// Keep track of how many items we have started searching at this depth
	self.waiters[depth].Add(1)

	// Add the searchable to the pool
	self.workerPool.Submit(func() {
		self.search(searchable, depth)
	})
}

func (self *ParallelSearch) search(searchable Searchable, depth int) {
	atomic.AddUint64(self.searched[depth], 1)
	if searchable.IsFound() {
		self.found <- searchable
	} else if depth < self.depthLimit { // Don't go past depthLimit
		searchable.Search(func(nextSearchable Searchable) {
			self.asyncSearch(nextSearchable, depth+1)
		})
	}
	// Mark this searchable has having been searched
	self.waiters[depth].Done()
}

func (self *ParallelSearch) announceDepthCompletion() {
	for depth, waiter := range self.waiters {
		waiter.Wait()
		if *self.searched[depth] > 0 {
			fmt.Println("================ FINISHED DEPTH ", depth, " [", *self.searched[depth], "] ==================")
		}
	}
	// If we've run out of searchables to consider, stop looking for more results
	close(self.found)
}
