<template>
  <div class="page-container">
    <a-card :title="$t('menu.projects')" :bordered="false">
      <a-row :gutter="16">
        <a-col :span="8">
          <a-form :model="projectForm" layout="vertical">
            <a-form-item label="Name">
              <a-input v-model="projectForm.name" data-testid="project-name" />
            </a-form-item>
            <a-form-item label="Webhook URL">
              <a-input v-model="projectForm.webhook_url" data-testid="project-webhook-url" />
            </a-form-item>
            <a-form-item label="IP whitelist">
              <a-input v-model="projectWhitelist" data-testid="project-ip-whitelist" placeholder="127.0.0.1, 10.0.0.0/8" />
            </a-form-item>
            <a-button type="primary" long data-testid="create-project" :loading="loading" @click="handleCreateProject">
              <template #icon><icon-plus /></template>
              Create Project
            </a-button>
          </a-form>
        </a-col>
        <a-col :span="16">
          <a-table row-key="id" :loading="loading" :data="projects" :columns="columns" />
        </a-col>
      </a-row>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { ProjectRecord, createProject, queryProjects } from '@/api/device-platform';

  defineOptions({ name: 'ProjectsIndex' });

  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const projectWhitelist = ref('');
  const projectForm = reactive({
    name: 'Smoke Test Project',
    webhook_url: 'https://example.com/device-webhook',
  });

  const columns = computed(() => [
    { title: 'Name', dataIndex: 'name' },
    { title: 'ID', dataIndex: 'id', ellipsis: true, tooltip: true },
    { title: 'Webhook', dataIndex: 'webhook_url', ellipsis: true, tooltip: true },
    { title: 'Created', dataIndex: 'created_at', ellipsis: true, tooltip: true },
  ]);

  const refresh = async () => {
    const res = await queryProjects();
    projects.value = res.data;
  };

  const handleCreateProject = async () => {
    setLoading(true);
    try {
      const whitelist = projectWhitelist.value
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean);
      await createProject({ ...projectForm, ip_whitelist: whitelist });
      await refresh();
      Message.success('Project created');
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
