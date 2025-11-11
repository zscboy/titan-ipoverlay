package socks5

import "sync"

type userUDPCount struct {
	users  map[string]int
	lock   sync.Mutex
	server *Socks5Server
}

func newUserUDPCount(server *Socks5Server) *userUDPCount {
	return &userUDPCount{users: make(map[string]int), server: server}
}

func (count *userUDPCount) incr(user string) int {
	count.lock.Lock()
	defer count.lock.Unlock()

	count.users[user] = count.users[user] + 1
	return count.users[user]
}

func (count *userUDPCount) decr(user string) int {
	count.lock.Lock()
	defer count.lock.Unlock()

	v, ok := count.users[user]
	if ok {
		v = v - 1
	}

	if v > 0 {
		count.users[user] = v
		return v
	}

	count.stopUserUDPServer(user)
	delete(count.users, user)
	return 0
}

func (count *userUDPCount) stopUserUDPServer(user string) {
	server := count.server
	v, ok := server.userUDPServers.Load(user)
	if !ok {
		return
	}

	udpServer := v.(*UDPServer)
	udpServer.stop()

	server.userUDPServers.Delete(user)
}
