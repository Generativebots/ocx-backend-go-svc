#!/bin/bash
# Compile jury.proto for Go and Python gRPC stubs.
# Prerequisites: protoc, protoc-gen-go, protoc-gen-go-grpc, grpcio-tools (Python)
set -e

PROTO_DIR="$(cd "$(dirname "$0")/.." && pwd)/pb/jury"
GO_OUT="$PROTO_DIR"
PY_OUT="$(cd "$(dirname "$0")/../../ocx-services-py-svc/ocx-services-py-svc" && pwd)/jury"

echo "📦 Compiling jury.proto..."

# Go stubs
echo "  → Go stubs..."
protoc \
  --go_out="$GO_OUT" --go_opt=paths=source_relative \
  --go-grpc_out="$GO_OUT" --go-grpc_opt=paths=source_relative \
  -I "$PROTO_DIR" \
  "$PROTO_DIR/jury.proto"

# Python stubs
echo "  → Python stubs..."
mkdir -p "$PY_OUT"
python3 -m grpc_tools.protoc \
  --python_out="$PY_OUT" \
  --grpc_python_out="$PY_OUT" \
  -I "$PROTO_DIR" \
  "$PROTO_DIR/jury.proto"

echo "✅ Proto compilation complete."
echo "   Go:     $GO_OUT/jury.pb.go, jury_grpc.pb.go"
echo "   Python: $PY_OUT/jury_pb2.py, jury_pb2_grpc.py"
