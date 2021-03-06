package rpc

import (
	"context"
	"errors"
	"github.com/micro/protobuf/ptypes"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"seqsvr/lib/grpcerr"
	"seqsvr/lib/logger"
	"seqsvr/protobuf/storesvr"
	"seqsvr/store/config"
	"seqsvr/store/err_status"
	"time"
)

const numAttempts = 3
const attemptInterval = 5 * time.Second

func Join(conf *config.RpcConf, raftAddr, nodeId string, meta map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	for i := 0; i < numAttempts; i++ {
		for _, addr := range conf.JoinAddr {
			if err := join(ctx, addr, raftAddr, nodeId, meta); err != nil {
				continue
			}
			return
		}
		logger.Infof("failed to join cluster at %s, sleeping %s before retry", conf.JoinAddr, attemptInterval)
		time.Sleep(attemptInterval)
	}
	logger.Panicf("failed to join cluster at %s, after %d attempts", conf.JoinAddr, numAttempts)

}

func join(ctx context.Context, addr, raftAddr, nodeId string, meta map[string]string) error {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		logger.Error("did not connect", zap.Error(err))
		return err
	}
	defer conn.Close()
	client := storesvr.NewStoreServerClient(conn)
	var md metadata.MD
	_, err = client.RpcJoin(ctx, &storesvr.ReqNodeJoin{Addr: raftAddr, NodeId: nodeId, Metadata: meta}, grpc.Trailer(&md))
	if err != nil {
		err := grpcerr.UnmarshalError(err, md)
		grpcErr, ok := err.(*grpcerr.Error)
		if ok && grpcErr != nil {
			if grpcErr.ErrCode == err_status.NotLeader {
				if len(grpcErr.Detail) > 0 {
					var leader storesvr.Leader
					if err = ptypes.UnmarshalAny(grpcErr.Detail[0], &leader); err != nil {
						logger.Error("ptypes UnmarshalAny", zap.Error(err))
						return err
					}
					if leader.Addr == "" {
						logger.Debug("not found leader", zap.String("node", addr))
						return errors.New("not found leader")
					}
					return join(ctx, leader.Addr, raftAddr, nodeId, meta)
				}
			}
		}
		logger.Error("RpcJoin", zap.Error(err))
		return err
	}
	return nil
}
