# out NDK home path
NDK_PATH=/opt/android-ndk-r27c

CC_PATH=$NDK_PATH/toolchains/llvm/prebuilt/linux-x86_64/bin
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

OUT_PATH=$SCRIPT_DIR/output

# our lib name
TARGET=client

# LDFLAGS='-s -w -extldflags -Wl,-soname,'"$TARGET"''


GOOS=android GOARCH=arm64 CGO_ENABLED=1 CC=$CC_PATH/aarch64-linux-android34-clang go build -o $OUT_PATH/bin/${TARGET}_arm64 ./client/

GOOS=android GOARCH=arm CGO_ENABLED=1 CC=$CC_PATH/armv7a-linux-androideabi34-clang go build -o $OUT_PATH/bin/${TARGET}_arm ./client/
