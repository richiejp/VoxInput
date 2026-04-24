# Copy GGML shared libraries from SRC_DIR to DST_DIR.
# Copies libggml*.dylib, libggml*.so, and their symlinks.
file(GLOB _ggml_libs
    "${SRC_DIR}/libggml*.dylib"
    "${SRC_DIR}/libggml*.so"
)
foreach(_lib ${_ggml_libs})
    get_filename_component(_name ${_lib} NAME)
    if(IS_SYMLINK ${_lib})
        file(READ_SYMLINK ${_lib} _target)
        if(NOT EXISTS "${DST_DIR}/${_name}" OR IS_SYMLINK "${DST_DIR}/${_name}")
            file(CREATE_LINK ${_target} "${DST_DIR}/${_name}" SYMBOLIC)
        endif()
    else()
        file(COPY_FILE ${_lib} "${DST_DIR}/${_name}" ONLY_IF_DIFFERENT)
    endif()
endforeach()
