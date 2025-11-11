package model

// type Browser struct {
// 	Id string
// 	// OS         string `redis:"os"`
// 	LoginAt    string `redis:"login_at"`
// 	RegisterAt string `redis:"register_at"`
// 	Online     bool
// 	IP         string `redis:"ip"`
// 	BindUser   string `redis:"bind_user"`
// }

// func SetBrowserAndZadd(ctx context.Context, redis *redis.Redis, browser *Browser) error {
// 	hashKey := fmt.Sprintf(redisKeyBrowser, browser.Id)
// 	m, err := structToMap(browser)
// 	if err != nil {
// 		return err
// 	}

// 	layout := "2006-01-02 15:04:05 -0700 MST"
// 	t, err := time.Parse(layout, browser.RegisterAt)
// 	if err != nil {
// 		return err
// 	}

// 	pipe, err := redis.TxPipeline()
// 	if err != nil {
// 		return err
// 	}

// 	pipe.HMSet(ctx, hashKey, m)
// 	pipe.ZAdd(ctx, redisKeyBrowserZset, goredis.Z{Score: float64(t.Unix()), Member: browser.Id})

// 	_, err = pipe.Exec(ctx)
// 	if err != nil {
// 		return err
// 	}

// 	return err
// }

// func SaveBrowser(redis *redis.Redis, browser *Browser) error {
// 	key := fmt.Sprintf(redisKeyBrowser, browser.Id)
// 	m, err := structToMap(browser)
// 	if err != nil {
// 		return err
// 	}

// 	logx.Infof("m:%v", m)
// 	return redis.Hmset(key, m)
// }

// // GetNode if node not exist, return nil
// func GetBrowser(redis *redis.Redis, id string) (*Browser, error) {
// 	key := fmt.Sprintf(redisKeyBrowser, id)
// 	m, err := redis.Hgetall(key)
// 	if err != nil {
// 		return nil, err
// 	}

// 	if len(m) == 0 {
// 		return nil, nil
// 	}

// 	browser := &Browser{}
// 	err = mapToStruct(m, browser)
// 	if err != nil {
// 		return nil, err
// 	}

// 	online, err := isBrowserOnline(redis, id)
// 	if err != nil {
// 		return nil, err
// 	}

// 	browser.Id = id
// 	browser.Online = online
// 	return browser, nil
// }

// func ListBrowser(ctx context.Context, redis *redis.Redis, start, end int) ([]*Node, error) {
// 	return listBrowser(ctx, redis, redisKeyBrowserZset, start, end)
// }

// func ListUnbindBrowser(ctx context.Context, redis *redis.Redis, start, end int) ([]*Node, error) {
// 	return listBrowser(ctx, redis, redisKeyNodeUnbind, start, end)
// }

// func ListBindBrowser(ctx context.Context, redis *redis.Redis, start, end int) ([]*Node, error) {
// 	return listBrowser(ctx, redis, redisKeyNodeBind, start, end)
// }

// func listBrowser(ctx context.Context, redis *redis.Redis, keyOfBrowserSortSet string, start, end int) ([]*Node, error) {
// 	ids, err := redis.Zrevrange(keyOfBrowserSortSet, int64(start), int64(end))
// 	if err != nil {
// 		return nil, err
// 	}

// 	pipe1, err := redis.TxPipeline()
// 	if err != nil {
// 		return nil, err
// 	}

// 	for _, id := range ids {
// 		key := fmt.Sprintf(redisKeyBrowserOnline, id)
// 		pipe1.Exists(ctx, key)
// 	}

// 	results, err := pipe1.Exec(ctx)
// 	if err != nil {
// 		return nil, err
// 	}

// 	onlines := make([]bool, 0, len(ids))
// 	for _, result := range results {
// 		exist, err := result.(*goredis.IntCmd).Result()
// 		if err != nil {
// 			logx.Errorf("ListNode parse result failed:%s", err.Error())
// 			continue
// 		}
// 		onlines = append(onlines, exist == 1)
// 	}

// 	pipe2, err := redis.TxPipeline()
// 	if err != nil {
// 		return nil, err
// 	}

// 	for _, id := range ids {
// 		key := fmt.Sprintf(redisKeyBrowser, id)
// 		pipe2.HGetAll(ctx, key)
// 	}

// 	cmds, err := pipe2.Exec(ctx)
// 	if err != nil {
// 		return nil, err
// 	}

// 	nodes := make([]*Node, 0, len(cmds))
// 	for i, cmd := range cmds {
// 		result, err := cmd.(*goredis.MapStringStringCmd).Result()
// 		if err != nil {
// 			logx.Errorf("ListNode parse result failed:%s", err.Error())
// 			continue
// 		}

// 		node := Node{Id: ids[i], Online: onlines[i]}
// 		err = mapToStruct(result, &node)
// 		if err != nil {
// 			logx.Errorf("ListNode mapToStruct error:%s", err.Error())
// 			continue
// 		}

// 		nodes = append(nodes, &node)
// 	}

// 	return nodes, nil
// }

// func GetBrowserLen(redis *redis.Redis) (int, error) {
// 	return redis.Zcard(redisKeyBrowserZset)
// }

// func GetUnbindBrowserLen(redis *redis.Redis) (int, error) {
// 	return redis.Zcard(redisKeyBrowserUnbind)
// }
// func GetbindBrowserLen(redis *redis.Redis) (int, error) {
// 	return redis.Zcard(redisKeyBrowserBind)
// }

// func SetBrowserOnline(redis *redis.Redis, nodeId string) error {
// 	key := fmt.Sprintf(redisKeyBrowserOnline, nodeId)
// 	if err := redis.Set(key, "true"); err != nil {
// 		return err
// 	}

// 	return redis.Expire(key, 60)
// }

// func SetBrowserOffline(redis *redis.Redis, nodeId string) error {
// 	key := fmt.Sprintf(redisKeyBrowserOnline, nodeId)
// 	_, err := redis.Del(key)
// 	return err
// }

// func isBrowserOnline(redis *redis.Redis, nodeId string) (bool, error) {
// 	key := fmt.Sprintf(redisKeyBrowserOnline, nodeId)
// 	return redis.Exists(key)
// }

// func BindBrowser(redis *redis.Redis, nodeID, userName string) error {
// 	_, err := redis.Zrem(redisKeyNodeUnbind, nodeID)
// 	if err != nil {
// 		return err
// 	}
// 	_, err = redis.Zadd(redisKeyNodeBind, time.Now().Unix(), nodeID)
// 	if err != nil {
// 		return err
// 	}

// 	node, err := GetNode(redis, nodeID)
// 	if err != nil {
// 		return err
// 	}

// 	if node.BindUser != userName {
// 		node.BindUser = userName
// 		return SaveNode(redis, node)
// 	}

// 	return nil
// }

// func UnbindNode(redis *redis.Redis, nodeID string) error {
// 	_, err := redis.Zrem(redisKeyNodeBind, nodeID)
// 	if err != nil {
// 		return err
// 	}
// 	_, err = redis.Zadd(redisKeyNodeUnbind, time.Now().Unix(), nodeID)
// 	if err != nil {
// 		return err
// 	}

// 	node, err := GetNode(redis, nodeID)
// 	if err != nil {
// 		return err
// 	}

// 	if len(node.BindUser) > 0 {
// 		node.BindUser = ""
// 		return SaveNode(redis, node)
// 	}

// 	return nil
// }
