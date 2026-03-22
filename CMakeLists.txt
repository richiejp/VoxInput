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

# Build libdeepvqe shared library from the submodule
set(DEEPVQE_BUILD_SHARED ON CACHE BOOL "" FORCE)
add_subdirectory(deepvqe-ggml/ggml)

# Build the Go binary after libdeepvqe
add_custom_target(voxinput ALL
    COMMAND go build -o ${VOXINPUT_BUILD_DIR}/voxinput .
    WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}
    DEPENDS deepvqe_shared
    COMMENT "Building VoxInput Go binary"
)

install(TARGETS deepvqe_shared LIBRARY DESTINATION lib)
install(PROGRAMS ${VOXINPUT_BUILD_DIR}/voxinput DESTINATION bin)
