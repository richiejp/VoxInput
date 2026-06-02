cmake_minimum_required(VERSION 3.20)
project(VoxInput LANGUAGES C CXX)

set(VOXINPUT_BUILD_DIR ${CMAKE_CURRENT_BINARY_DIR})

find_package(Git REQUIRED)
execute_process(
    COMMAND ${GIT_EXECUTABLE} submodule update --init --recursive
    WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}
    RESULT_VARIABLE GIT_SUBMODULE_RESULT
)
if(NOT GIT_SUBMODULE_RESULT EQUAL 0)
    message(FATAL_ERROR "git submodule update --init --recursive failed")
endif()

# Build liblocalvqe shared library from the submodule.
# LocalVQE optionally links libsndfile for WAV I/O in its CLI/tests; the
# shared library used by VoxInput does not need it, and pkg-config on some
# systems reports sndfile as present when the linker cannot actually find it,
# so skip that discovery entirely.
set(LOCALVQE_BUILD_SHARED ON CACHE BOOL "" FORCE)
set(CMAKE_DISABLE_FIND_PACKAGE_PkgConfig TRUE CACHE BOOL "" FORCE)
add_subdirectory(LocalVQE/ggml)

# Download the LocalVQE GGUF models. Both are bundled; v1.2 is the runtime
# default and v1.3 is selectable via VOXINPUT_LOCALVQE_MODEL_VERSION. Variant
# names and checksums must stay in sync with internal/localvqe/model.go.
set(LOCALVQE_MODEL_BASE_URL "https://huggingface.co/LocalAI-io/LocalVQE/resolve/main")
set(LOCALVQE_MODEL_DIR "${CMAKE_CURRENT_BINARY_DIR}/share/voxinput")

function(download_localvqe_model variant sha256)
    file(DOWNLOAD
        "${LOCALVQE_MODEL_BASE_URL}/localvqe-${variant}-f32.gguf"
        "${LOCALVQE_MODEL_DIR}/localvqe-${variant}-f32.gguf"
        EXPECTED_HASH SHA256=${sha256}
        SHOW_PROGRESS
    )
endfunction()

download_localvqe_model("v1.2-1.3M" "4856ecf5f522b23fb2bc5caeac81f323c0ef1c4c156a9c7d40a6adbe092ba9ce")
download_localvqe_model("v1.3-4.8M" "c4f7912485c32cfc206c536f2f050b52513f2f613fdbc616391f6b26ab1d51ec")

# Copy liblocalvqe and GGML runtime libraries next to the binary so
# dlopen finds them at runtime, then build the Go binary.
# The GGML backend loader searches the executable's directory for
# libggml-cpu-*.so variant files.
set(GGML_BIN_DIR ${CMAKE_CURRENT_BINARY_DIR}/bin)
add_custom_target(voxinput ALL
    COMMAND ${CMAKE_COMMAND} -E copy
        $<TARGET_FILE:localvqe_shared>
        ${VOXINPUT_BUILD_DIR}/$<TARGET_FILE_NAME:localvqe_shared>
    COMMAND ${CMAKE_COMMAND} -E create_symlink
        $<TARGET_FILE_NAME:localvqe_shared>
        ${VOXINPUT_BUILD_DIR}/$<TARGET_LINKER_FILE_NAME:localvqe_shared>
    COMMAND ${CMAKE_COMMAND}
        -DSRC_DIR=${GGML_BIN_DIR}
        -DDST_DIR=${VOXINPUT_BUILD_DIR}
        -P ${CMAKE_CURRENT_SOURCE_DIR}/cmake/copy_ggml_libs.cmake
    COMMAND go build -o ${VOXINPUT_BUILD_DIR}/voxinput .
    WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}
    DEPENDS localvqe_shared
    COMMENT "Copying liblocalvqe and building VoxInput Go binary"
)

install(TARGETS localvqe_shared LIBRARY DESTINATION lib)
install(PROGRAMS ${VOXINPUT_BUILD_DIR}/voxinput DESTINATION bin)
install(FILES
    ${LOCALVQE_MODEL_DIR}/localvqe-v1.2-1.3M-f32.gguf
    ${LOCALVQE_MODEL_DIR}/localvqe-v1.3-4.8M-f32.gguf
    DESTINATION share/voxinput)
