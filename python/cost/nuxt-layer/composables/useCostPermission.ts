import { ref, computed, onMounted, Ref } from 'vue'

export const useCostPermission = () => {
  const user = useState<any>('auth-user')
  const config = useRuntimeConfig()
  const apiBase = computed(() =>
    config.public.costOnly ? '' : ((config.public.apiBase as string) || '')
  )

  const permissions = ref<string[]>([])
  const roles = ref<any[]>([])
  const loading = ref(true)

  async function fetchMyPermissions() {
    loading.value = true
    try {
      const email = user.value?.email
      if (!email) {
        permissions.value = []
        roles.value = []
        return
      }
      const data: any = await $fetch(`${apiBase.value}/api/cost/roles/my`, {
        headers: { 'X-Cost-User': email },
      })
      permissions.value = data.permissions || []
      roles.value = data.roles || []
    } catch {
      permissions.value = []
      roles.value = []
    } finally {
      loading.value = false
    }
  }

  function can(perm: string): boolean {
    return permissions.value.includes(perm)
  }

  onMounted(() => {
    fetchMyPermissions()
  })

  return { permissions, roles, loading, can, fetchMyPermissions }
}
