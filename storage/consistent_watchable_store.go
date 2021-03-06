// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"encoding/binary"
	"log"
)

var (
	consistentIndexKeyName = []byte("consistent_index")
)

// ConsistentIndexGetter is an interface that wraps the Get method.
// Consistent index is the offset of an entry in a consistent replicated log.
type ConsistentIndexGetter interface {
	// ConsistentIndex returns the consistent index of current executing entry.
	ConsistentIndex() uint64
}

type consistentWatchableStore struct {
	*watchableStore
	// The field is used to get the consistent index of current
	// executing entry.
	// When the store finishes executing current entry, it will
	// put the index got from ConsistentIndexGetter into the
	// underlying backend. This helps to recover consistent index
	// when restoring.
	ig ConsistentIndexGetter
}

func New(path string, ig ConsistentIndexGetter) ConsistentWatchableKV {
	return newConsistentWatchableStore(path, ig)
}

// newConsistentWatchableStore creates a new consistentWatchableStore
// using the file at the given path.
// If the file at the given path does not exist then it will be created automatically.
func newConsistentWatchableStore(path string, ig ConsistentIndexGetter) *consistentWatchableStore {
	return &consistentWatchableStore{
		watchableStore: newWatchableStore(path),
		ig:             ig,
	}
}

func (s *consistentWatchableStore) Put(key, value []byte) (rev int64) {
	id := s.TxnBegin()
	rev, err := s.TxnPut(id, key, value)
	if err != nil {
		log.Panicf("unexpected TxnPut error (%v)", err)
	}
	if err := s.TxnEnd(id); err != nil {
		log.Panicf("unexpected TxnEnd error (%v)", err)
	}
	return rev
}

func (s *consistentWatchableStore) DeleteRange(key, end []byte) (n, rev int64) {
	id := s.TxnBegin()
	n, rev, err := s.TxnDeleteRange(id, key, end)
	if err != nil {
		log.Panicf("unexpected TxnDeleteRange error (%v)", err)
	}
	if err := s.TxnEnd(id); err != nil {
		log.Panicf("unexpected TxnEnd error (%v)", err)
	}
	return n, rev
}

func (s *consistentWatchableStore) TxnBegin() int64 {
	id := s.watchableStore.TxnBegin()

	// TODO: avoid this unnecessary allocation
	bs := make([]byte, 8)
	binary.BigEndian.PutUint64(bs, s.ig.ConsistentIndex())
	// put the index into the underlying backend
	// tx has been locked in TxnBegin, so there is no need to lock it again
	s.watchableStore.store.tx.UnsafePut(metaBucketName, consistentIndexKeyName, bs)

	return id
}

func (s *consistentWatchableStore) ConsistentIndex() uint64 {
	tx := s.watchableStore.store.b.BatchTx()
	tx.Lock()
	defer tx.Unlock()

	// get the index
	_, vs := tx.UnsafeRange(metaBucketName, consistentIndexKeyName, nil, 0)
	if len(vs) == 0 {
		return 0
	}
	return binary.BigEndian.Uint64(vs[0])
}
