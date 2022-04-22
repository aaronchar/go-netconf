package junos_helpers

import (
	"fmt"
	"sync"
)

type BatchHelper interface {
	AddToReadMap(in string) error
	AddToWriteMap(in string) error
	AddToDeleteMap(in string) error
	QueryGroupXMLFromCache(id string) (string, error)
	QueryGroupReadMap(id string) string
	QueryGroupWriteMap(id string) string
	QueryGroupDeleteMap(id string) string
	QueryAllGroupReads() string
	QueryAllGroupWrites() string
	QueryAllGroupDeletes() string
	IsHydrated() bool
}
type batchHelper struct {
	readCacheMap        *sync.Map
	writeCacheMap       *sync.Map
	deleteCacheMap      *sync.Map
	readFullCache       string
	readGroupIsHydrated bool
}

func NewBatchHelper() BatchHelper {
	return &batchHelper{
		readCacheMap:   &sync.Map{},
		writeCacheMap:  &sync.Map{},
		deleteCacheMap: &sync.Map{},
	}
}

func (b *batchHelper) AddToReadMap(in string) error {
	applyGroupNodes, err := findGroupInDoc(in, fmt.Sprintf("//groups/name"))
	if err != nil {
		return err
	}
	for _, v := range applyGroupNodes {
		k := v.InnerText()
		ev, _ := b.readCacheMap.LoadOrStore(k, "")
		nv := ev.(string)
		nv += v.Parent.OutputXML(true)

		b.readCacheMap.Store(k, nv)
	}
	b.readGroupIsHydrated = true
	return nil
}
func (b *batchHelper) AddToWriteMap(in string) error {
	payload := batchConfigReplacer.Replace(in)
	// we need to strip off the <configuration> blocks since we want to send this \
	// as one large configuration push without changing the way the upstream system works
	groupName, err := findApplyGroupName(in)
	if err != nil {
		return err
	}
	ev, _ := b.writeCacheMap.LoadOrStore(groupName, "")
	nv := ev.(string)
	nv += payload
	b.writeCacheMap.Store(groupName, nv)
	return nil
}
func (b *batchHelper) AddToDeleteMap(in string) error {
	payload := fmt.Sprintf(batchDeletePayload, in)

	groupName, err := findApplyGroupName(payload)
	if err != nil {
		return err
	}
	ev, _ := b.deleteCacheMap.LoadOrStore(groupName, "")
	nv := ev.(string)
	nv += payload
	b.deleteCacheMap.Store(groupName, nv)
	return nil
}
func (b *batchHelper) QueryGroupXMLFromCache(id string) (string, error) {

	if writeElements, found := b.writeCacheMap.Load(id); found {
		var out string
		e := writeElements.(string)
		out += e
		return fmt.Sprintf(batchReadWrapper, out), nil
	}
	if readElements, found := b.readCacheMap.Load(id); found {
		var out string
		e := readElements.(string)
		out += e
		return fmt.Sprintf(batchReadWrapper, out), nil
	}
	return "", nil
}
func (b *batchHelper) QueryGroupReadMap(id string) string {
	var out string
	if ev, ok := b.readCacheMap.Load(id); ok {
		e := ev.(string)
		out += e
	}
	return out
}
func (b *batchHelper) QueryGroupWriteMap(id string) string {
	var out string
	if ev, ok := b.writeCacheMap.Load(id); ok {
		e := ev.(string)
		out += e
	}
	return out
}
func (b *batchHelper) QueryGroupDeleteMap(id string) string {
	var out string
	if ev, ok := b.deleteCacheMap.Load(id); ok {
		e := ev.(string)
		out += e
	}
	return out
}
func (b *batchHelper) QueryAllGroupReads() string {
	var out string
	b.readCacheMap.Range(func(k interface{}, v interface{}) bool {
		s := v.(string)
		out += s
		return true
	})
	return out
}
func (b *batchHelper) QueryAllGroupWrites() string {
	var out string
	b.writeCacheMap.Range(func(k interface{}, v interface{}) bool {
		s := v.(string)
		out += s
		return true
	})
	return out
}
func (b *batchHelper) QueryAllGroupDeletes() string {
	var out string
	b.deleteCacheMap.Range(func(k interface{}, v interface{}) bool {
		s := v.(string)
		out += s
		return true
	})
	return out
}

func (b *batchHelper) IsHydrated() bool {
	return b.readGroupIsHydrated
}
