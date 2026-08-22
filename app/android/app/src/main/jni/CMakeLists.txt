cmake_minimum_required(VERSION 3.22.1)
project(daidai_pty C)

add_library(daidai_pty SHARED pty_bridge.c)
target_compile_options(daidai_pty PRIVATE -Wall -Wextra -Werror)
target_link_options(daidai_pty PRIVATE
    "LINKER:-z,max-page-size=16384"
    "LINKER:-z,common-page-size=16384")
