#!/bin/zsh -f
set -eu
task_login_dir="${0:A:h}"
/usr/bin/python3 -I "$task_login_dir/private-runtime-login.py" prepare --runtime grok
exec /usr/bin/python3 -I "$task_login_dir/private-runtime-login.py" login --runtime grok
