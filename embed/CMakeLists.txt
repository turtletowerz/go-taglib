cmake_minimum_required(VERSION 3.5.0 FATAL_ERROR)
project(taglib_wasm)

add_subdirectory(
  taglib
)
include_directories(
  taglib/taglib
  taglib/taglib/toolkit
)

add_executable(taglib taglib_wasm.cpp)
set_target_properties(taglib PROPERTIES SUFFIX ".wasm" C_VISIBILITY_PRESET hidden)
target_compile_options(taglib PRIVATE --target=wasm32-wasi -g0 -O2)
target_link_options(taglib PRIVATE -Wl,--no-entry -Wl,--allow-undefined -Wl,@${CMAKE_CURRENT_SOURCE_DIR}/exports.txt)
target_link_libraries(taglib PRIVATE tag)
