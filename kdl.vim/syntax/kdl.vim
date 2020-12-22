" keywords
syntax keyword kdlGroupVersionK groupVersion
syntax keyword kdlBlockTypeK kind struct enum union
syntax keyword kdlNewtypeK newtype

" Param list & operators
syntax region kdlParamList start="(" end=")" contains=kdlParamKey,kdlColon,kdlComma,@kdlValue
syntax match kdlParamKey '\<\l\(\l\|\d\|-\)*\>' contained nextgroup=kdlColon
syntax match kdlColon ':' contained
syntax match kdlComma ',' contained

" values
syntax match kdlNumber '-\?\d\+' contained
syntax keyword kdlBool true false contained
syn region kdlStruct start="{" end="}" contains=kdlColon,kdlParamKey,kdlComma,@kdlValue contained
syn region kdlList start="\[" end="\]" contains=kdlComma,@kdlValue contained
syntax match kdlTypeIdent '\<\u\(\a\|\d\)*\>'
syntax match kdlFieldIdent '\<\l\(\a\|\d\)*\>'
syntax match kdlFieldPath '\.\l\(\a\|\d\)*\>' contained
syntax region kdlString start=/"/ end=/"/ skip=`\v\\\\|\\"` contained
syntax cluster kdlValue contains=kdlNumber,kdlBool,kdlString,kdlStruct,kdlList,kdlTypeIdent,kdlFieldPath,kdlInlineTypeK,kdlInlineTypeM,kdlPrimitiveTypeK,kdlDangerousTypeK,kdlParamList

" types
syntax keyword kdlInlineTypeK set nextgroup=kdlParamList
syntax match kdlInlineTypeM 'list-map\|list\|simple-map' nextgroup=kdlParamList
syntax keyword kdlPrimitiveTypeK string time duration int32 int64 quantity
syntax keyword kdlCompoundModK optional as nextgroup=kdlParamList
syntax match kdlSimpleModK "create-only\|inline"
syntax keyword kdlDangerousTypeK dangerousfloat64
syntax cluster kdlTypeSpec contains=kdlInlineTypeK,kdlInlineTypeM,kdlPrimitiveTypeK,kdlCompoundModK,kdlSimpleModK,kdlParamList,kdlDangerousTypeK

" markers
syntax match kdlMarker '@\l\(\l\|\d\|-\|::\)*\>'
"
" Blocks
syn region kdlBlock start="{" end="}" contains=TOP fold

" Comments
" NB: later definitions are higher priority -- order matters here
syn match kdlLineComment "//.*$" contains=kdlTODO
syn region kdlBlockComment start="/\*" end="\*/"
syn match kdlDoc "^\s*///.*$"
syn match kdlDocHeading "^\s*/// #.*$"
syn cluster kdlComments contains=kdlLineComment,kdlBlockComment
syn cluster kdlDocs contains=kdlDoc,kdlDocHeading
syn keyword kdlTODO contained TODO NB

" highlighting
hi def link kdlBlockComment Comment
hi def link kdlLineComment Comment
hi def link kdlTODO Todo
hi def link kdlDoc SpecialComment
hi def link kdlDocHeading SpecialComment

hi def link kdlParamKey Identifier
hi def link kdlComma Operator
hi def link kdlColon Operator

"hi def link kdlTypeIdent Type
hi def link kdlFieldIdent Identifier

hi def link kdlNumber Number
hi def link kdlString String
hi def link kdlBool Boolean
hi def link kdlFloat Float
hi def link kdlFieldPath Identifier

hi def link kdlGroupVersionK Keyword
hi def link kdlBlockTypeK Structure
hi def link kdlNewtypeK Typedef

hi def link kdlInlineTypeK Type
hi def link kdlInlineTypeM Type
hi def link kdlPrimitiveTypeK Type
hi def link kdlCompoundModK StorageClass
hi def link kdlSimpleModK StorageClass
hi def link kdlDangerousTypeK Type

hi def link kdlMarker PreProc
