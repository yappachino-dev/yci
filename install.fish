#!/usr/bin/fish

set APP_NAME "yci"
set BIN_DIR "$HOME/.local/bin"
set COMP_DIR "$HOME/.config/fish/completions"

echo " Building $APP_NAME..."
go build -ldflags="-s -w" -o $APP_NAME .

if test $status -eq 0
    echo " Moving binary to $BIN_DIR..."
    mkdir -p $BIN_DIR
    mv $APP_NAME $BIN_DIR/

    echo " Generating Fish completions..."
    mkdir -p $COMP_DIR
    $BIN_DIR/$APP_NAME completion fish > $COMP_DIR/$APP_NAME.fish

    echo " Done! Refreshing shell..."
    source ~/.config/fish/config.fish
else
    echo " Build failed."
    exit 1
end
