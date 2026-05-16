<template>
  <a-layout-footer class="footer-container">
    <div class="footer-item">Copyright © 2025 admin9.com All Rights Reserved </div>
    <div class="footer-item">西昌齐跃网络科技有限责任公司</div>
    <div class="flex flex-col">
      <a-link href="https://beian.miit.gov.cn/" target="_blank" class="ml-1">
        <template #icon>
          <img
            width="16"
            src="data:image/png;base64,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"
          />
        </template>
        蜀ICP备2022018286号-1
      </a-link>
    </div>
  </a-layout-footer>
</template>

<script lang="ts" setup></script>

<style lang="less" scoped>
  .footer-container {
    @apply w-full h-24 lg:h-10 flex flex-col lg:flex-row justify-center items-center text-center text-sm gap-2 lg:gap-1;

    color: var(--color-text-2);
  }
</style>
