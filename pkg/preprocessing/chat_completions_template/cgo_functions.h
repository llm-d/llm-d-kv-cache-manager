/*
Copyright 2025 The llm-d Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

#ifndef CGO_FUNCTIONS_H
#define CGO_FUNCTIONS_H

#include <Python.h>
#include <stdio.h>
#include <stdlib.h>

// Initialize Python interpreter
int Py_InitializeGo() {
    Py_Initialize();
    return 0;
}

// Finalize Python interpreter
void Py_FinalizeGo() {
    Py_Finalize();
}

// CGo cannot call C macros, so we wrap PyRun_SimpleString in a function
int Go_PyRun_SimpleString(const char* code) {
    return PyRun_SimpleString(code);
}

// Wrapper for PyImport_AddModule
PyObject* Go_PyImport_AddModule(const char* name) {
    return PyImport_AddModule(name);
}

// Wrapper for PyModule_GetDict
PyObject* Go_PyModule_GetDict(PyObject* module) {
    return PyModule_GetDict(module);
}

// Wrapper for PyDict_GetItemString
PyObject* Go_PyDict_GetItemString(PyObject* dict, const char* key) {
    return PyDict_GetItemString(dict, key);
}

// Helper function to convert Python string to Go string
const char* PyUnicode_AsGoString(PyObject* obj) {
    return PyUnicode_AsUTF8(obj);
}

#endif // CGO_FUNCTIONS_H 