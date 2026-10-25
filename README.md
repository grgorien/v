# v - use vim motions and natively manage your socks

Previously written in bash but it has some limitations that this repo will aim to aid. 

- process spawning
- parsing text tools to fragile (ss -xlp | awk and pstree | grep -oP)
    - potential solution: inodes /proc/net/tcp
- (-k) flag is a bomb that didn't explode (--remote-send '<Esc>:qa!<CR>') (bad injection)
- prune default from stale sockets
- parsing hacking (pstree -s $$ | grep -q nvim)
- --load is broken (never fixed) only matches basename because of -name (find -name '**/*.c')
