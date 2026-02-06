import React, { useRef, useEffect, useState } from 'react'
import Editor, { OnMount, OnChange } from '@monaco-editor/react'
import {
  Box,
  Button,
  HStack,
  VStack,
  Alert,
  AlertIcon,
  AlertDescription,
  Spinner,
  Text,
  useColorMode,
  Badge,
} from '@chakra-ui/react'
import { CheckIcon, WarningIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import type { editor } from 'monaco-editor'

interface YamlEditorProps {
  value: string
  onChange?: (value: string) => void
  onValidate?: (isValid: boolean, error?: string) => void
  readOnly?: boolean
  height?: string | number
  showValidationStatus?: boolean
}

const YamlEditor: React.FC<YamlEditorProps> = ({
  value,
  onChange,
  onValidate,
  readOnly = false,
  height = '500px',
  showValidationStatus = true,
}) => {
  const { t } = useTranslation()
  const { colorMode } = useColorMode()
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [validationError, setValidationError] = useState<string | null>(null)
  const [isValid, setIsValid] = useState(true)
  const [hasChanges, setHasChanges] = useState(false)
  const originalValueRef = useRef(value)

  // 當外部 value 变化時更新原始值
  useEffect(() => {
    originalValueRef.current = value
    setHasChanges(false)
  }, [value])

  const handleEditorDidMount: OnMount = (editor, monaco) => {
    editorRef.current = editor
    setIsLoading(false)

    // 配置 YAML 语言設置
    monaco.languages.setLanguageConfiguration('yaml', {
      comments: {
        lineComment: '#',
      },
      brackets: [
        ['{', '}'],
        ['[', ']'],
      ],
      autoClosingPairs: [
        { open: '{', close: '}' },
        { open: '[', close: ']' },
        { open: '"', close: '"' },
        { open: "'", close: "'" },
      ],
    })

    // 設置编辑器选项
    editor.updateOptions({
      minimap: { enabled: false },
      lineNumbers: 'on',
      renderLineHighlight: 'all',
      scrollBeyondLastLine: false,
      wordWrap: 'on',
      fontSize: 13,
      tabSize: 2,
      insertSpaces: true,
      folding: true,
      foldingStrategy: 'indentation',
      automaticLayout: true,
      readOnly: readOnly,
    })
  }

  const handleEditorChange: OnChange = (newValue) => {
    const val = newValue || ''
    
    // 检查是否有变化
    setHasChanges(val !== originalValueRef.current)
    
    // 調用外部 onChange
    if (onChange) {
      onChange(val)
    }

    // 基本的 YAML 语法检查
    validateYaml(val)
  }

  const validateYaml = (content: string) => {
    try {
      // 简單的语法检查：检查缩進一致性和基本格式
      const lines = content.split('\n')
      let error: string | null = null

      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        
        // 跳過空行和注释
        if (line.trim() === '' || line.trim().startsWith('#')) {
          continue
        }

        // 检查是否使用了 tab 缩進
        if (line.startsWith('\t')) {
          error = t('yamlEditor.tabIndentError', { line: i + 1 })
          break
        }

        // 檢查冒號後是否有值（若在同一行）。列表項（如 - ::1）整行是值，冒號可能是 IPv6 等的一部分，不當作 key: value 檢查
        const isListItemWithValue = /^\s*-\s+\S/.test(line)
        if (isListItemWithValue) {
          continue
        }
        const colonIndex = line.indexOf(':')
        if (colonIndex > 0) {
          const afterColon = line.substring(colonIndex + 1)
          if (afterColon.length > 0 && !afterColon.startsWith(' ') && afterColon.trim() !== '') {
            error = t('yamlEditor.colonSpaceError', { line: i + 1 })
            break
          }
        }
      }

      if (error) {
        setValidationError(error)
        setIsValid(false)
        onValidate?.(false, error)
      } else {
        setValidationError(null)
        setIsValid(true)
        onValidate?.(true)
      }
    } catch (e) {
      const errorMsg = e instanceof Error ? e.message : t('yamlEditor.unknownError')
      setValidationError(errorMsg)
      setIsValid(false)
      onValidate?.(false, errorMsg)
    }
  }

  // 初始驗证
  useEffect(() => {
    if (value) {
      validateYaml(value)
    }
  }, [])

  return (
    <VStack spacing={3} align="stretch" h="100%">
      {/* 状態欄 */}
      {showValidationStatus && (
        <HStack justify="space-between" px={2}>
          <HStack spacing={3}>
            {isValid ? (
              <Badge colorScheme="green" display="flex" alignItems="center" gap={1}>
                <CheckIcon boxSize={3} />
                <Text>{t('yamlEditor.syntaxCorrect')}</Text>
              </Badge>
            ) : (
              <Badge colorScheme="red" display="flex" alignItems="center" gap={1}>
                <WarningIcon boxSize={3} />
                <Text>{t('yamlEditor.syntaxError')}</Text>
              </Badge>
            )}
            {hasChanges && (
              <Badge colorScheme="yellow">
                {t('yamlEditor.unsavedChanges')}
              </Badge>
            )}
            {readOnly && (
              <Badge colorScheme="gray">
                {t('yamlEditor.readOnly')}
              </Badge>
            )}
          </HStack>
        </HStack>
      )}

      {/* 錯誤提示 */}
      {validationError && (
        <Alert status="error" borderRadius="md" py={2}>
          <AlertIcon />
          <AlertDescription fontSize="sm">{validationError}</AlertDescription>
        </Alert>
      )}

      {/* 编辑器容器 */}
      <Box
        borderRadius="lg"
        overflow="hidden"
        border="1px solid"
        borderColor={colorMode === 'dark' ? 'gray.600' : 'gray.200'}
        position="relative"
        flex="1"
      >
        {isLoading && (
          <Box
            position="absolute"
            top="0"
            left="0"
            right="0"
            bottom="0"
            display="flex"
            alignItems="center"
            justifyContent="center"
            bg={colorMode === 'dark' ? 'gray.800' : 'white'}
            zIndex={10}
          >
            <Spinner size="lg" />
          </Box>
        )}
        <Editor
          height={height}
          defaultLanguage="yaml"
          value={value}
          theme={colorMode === 'dark' ? 'vs-dark' : 'light'}
          onMount={handleEditorDidMount}
          onChange={handleEditorChange}
          options={{
            readOnly: readOnly,
            minimap: { enabled: false },
            lineNumbers: 'on',
            fontSize: 13,
            tabSize: 2,
            insertSpaces: true,
            wordWrap: 'on',
            scrollBeyondLastLine: false,
            automaticLayout: true,
          }}
          loading={<Spinner size="lg" />}
        />
      </Box>
    </VStack>
  )
}

export default YamlEditor
