package camera

import "context"

type Service interface {
	Start(ctx context.Context) error
}
