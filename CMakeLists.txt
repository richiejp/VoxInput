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

# Download the DeepVQE GGUF model
set(DEEPVQE_MODEL_URL "https://huggingface.co/richiejp/deepvqe-aec-gguf/resolve/main/deepvqe.gguf")
set(DEEPVQE_MODEL_PATH "${CMAKE_CURRENT_BINARY_DIR}/share/voxinput/deepvqe.gguf")
file(DOWNLOAD
    ${DEEPVQE_MODEL_URL}
    ${DEEPVQE_MODEL_PATH}
    EXPECTED_HASH SHA256=e0577d1a062c9219db64e9174a8e4c7d327566fbc850498f332601238736b084
    SHOW_PROGRESS
)

# Build the Go binary after libdeepvqe
add_custom_target(voxinput ALL
    COMMAND go build -o ${VOXINPUT_BUILD_DIR}/voxinput .
    WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}
    DEPENDS deepvqe_shared
    COMMENT "Building VoxInput Go binary"
)

install(TARGETS deepvqe_shared LIBRARY DESTINATION lib)
install(PROGRAMS ${VOXINPUT_BUILD_DIR}/voxinput DESTINATION bin)
install(FILES ${DEEPVQE_MODEL_PATH} DESTINATION share/voxinput)
