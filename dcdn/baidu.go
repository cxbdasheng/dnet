package dcdn

import "github.com/cxbdasheng/dnet/config"

type Baidu struct {
	CDN   *config.CDN
	Cache *Cache
}

func (baidu *Baidu) UpdateOrCreateSources() bool {
	//TODO implement me
	panic("implement me")
}

func (baidu *Baidu) Init(cdnConfig *config.CDN, cache *Cache) {
	baidu.CDN = cdnConfig
	baidu.Cache = cache
}
