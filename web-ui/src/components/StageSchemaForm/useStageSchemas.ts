import { useState, useEffect } from 'react'
import { api } from '../../services/api'
import type { StageSchema, CategoryInfo, FieldTypeInfo } from '../../types/stage-schema'

// Stage Schema 목록 조회 Hook
export function useStageSchemas() {
  const [data, setData] = useState<StageSchema[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  useEffect(() => {
    let isMounted = true

    const fetchSchemas = async () => {
      try {
        setIsLoading(true)
        const schemas = await api.getStageSchemas()
        if (isMounted) {
          setData(schemas)
          setError(null)
        }
      } catch (err) {
        if (isMounted) {
          setError(err instanceof Error ? err : new Error('Failed to fetch schemas'))
        }
      } finally {
        if (isMounted) {
          setIsLoading(false)
        }
      }
    }

    fetchSchemas()

    return () => {
      isMounted = false
    }
  }, [])

  return { data, isLoading, error }
}

// 특정 Stage Schema 조회 Hook
export function useStageSchema(type: string | undefined) {
  const [data, setData] = useState<StageSchema | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<Error | null>(null)

  useEffect(() => {
    if (!type) {
      setData(null)
      return
    }

    let isMounted = true

    const fetchSchema = async () => {
      try {
        setIsLoading(true)
        const schema = await api.getStageSchema(type)
        if (isMounted) {
          setData(schema)
          setError(null)
        }
      } catch (err) {
        if (isMounted) {
          setError(err instanceof Error ? err : new Error('Failed to fetch schema'))
        }
      } finally {
        if (isMounted) {
          setIsLoading(false)
        }
      }
    }

    fetchSchema()

    return () => {
      isMounted = false
    }
  }, [type])

  return { data, isLoading, error }
}

// 카테고리 목록 조회 Hook
export function useStageCategories() {
  const [data, setData] = useState<CategoryInfo[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  useEffect(() => {
    let isMounted = true

    const fetchCategories = async () => {
      try {
        setIsLoading(true)
        const categories = await api.getStageCategories()
        if (isMounted) {
          setData(categories)
          setError(null)
        }
      } catch (err) {
        if (isMounted) {
          setError(err instanceof Error ? err : new Error('Failed to fetch categories'))
        }
      } finally {
        if (isMounted) {
          setIsLoading(false)
        }
      }
    }

    fetchCategories()

    return () => {
      isMounted = false
    }
  }, [])

  return { data, isLoading, error }
}

// 필드 타입 목록 조회 Hook
export function useStageFieldTypes() {
  const [data, setData] = useState<FieldTypeInfo[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  useEffect(() => {
    let isMounted = true

    const fetchFieldTypes = async () => {
      try {
        setIsLoading(true)
        const fieldTypes = await api.getStageFieldTypes()
        if (isMounted) {
          setData(fieldTypes)
          setError(null)
        }
      } catch (err) {
        if (isMounted) {
          setError(err instanceof Error ? err : new Error('Failed to fetch field types'))
        }
      } finally {
        if (isMounted) {
          setIsLoading(false)
        }
      }
    }

    fetchFieldTypes()

    return () => {
      isMounted = false
    }
  }, [])

  return { data, isLoading, error }
}
