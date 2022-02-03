package rtsp

type Method string

const (
	MethodOptions      Method = "OPTIONS"
	MethodSetup        Method = "SETUP"
	MethodTeardown     Method = "TEARDOWN"
	MethodDescribe     Method = "DESCRIBE"
	MethodPlay         Method = "PLAY"
	MethodGetParameter Method = "GET_PARAMETER"
	MethodAnnounce     Method = "ANNOUNCE"
)

func (m Method) String() string {
	return string(m)
}
