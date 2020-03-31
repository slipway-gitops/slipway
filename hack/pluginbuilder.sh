TYPES="${TYPES:-internal/plugins/*}"
for f in $TYPES
do
  BINDIR="$(basename -- $f)"
  mkdir -p internal/bin/$BINDIR
  echo "Looping through plugins of type $f"
  PLUGINS=$f/*
  for p in $PLUGINS
  do
    echo "Building $p"
    SOFILE="$(basename -- $p)"
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -buildmode=plugin -o \
	internal/bin/$BINDIR/$SOFILE.so $p/$SOFILE.go
  done
done
