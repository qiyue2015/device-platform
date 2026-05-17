import { computed } from 'vue';
import { RouteRecordRaw, RouteRecordNormalized } from 'vue-router';
import type { RouteMeta } from 'vue-router';
import usePermission from '@/hooks/permission';
import { useAppStore } from '@/store';
import appClientMenus from '@/router/app-menus';
import { cloneDeep } from 'lodash';

const shouldFlattenSingleChild = (route: RouteRecordRaw, layer: number) => {
  return layer === 0 && route.meta?.flattenSingleChild !== false;
};

const inheritSingleChildMeta = (parent: RouteRecordRaw, child: RouteRecordRaw) => {
  const parentMeta = parent.meta || ({} as RouteMeta);
  const childMeta = child.meta || ({} as RouteMeta);

  return {
    ...child,
    meta: {
      ...childMeta,
      icon: childMeta.icon || parentMeta.icon,
      order: childMeta.order ?? parentMeta.order,
      menuGroup: childMeta.menuGroup || parentMeta.menuGroup,
      menuGroupOrder: childMeta.menuGroupOrder ?? parentMeta.menuGroupOrder,
    },
  };
};

const groupTopLevelMenus = (routes: RouteRecordRaw[]) => {
  const groupedRoutes: RouteRecordRaw[] = [];
  const groupMap = new Map<string, RouteRecordRaw>();

  routes.forEach((route) => {
    const menuGroup = route.meta?.menuGroup;
    if (!menuGroup) {
      groupedRoutes.push(route);
      return;
    }

    const groupRoute =
      groupMap.get(menuGroup) ||
      ({
        path: menuGroup,
        name: `MenuGroup:${menuGroup}`,
        meta: {
          locale: menuGroup,
          requiresAuth: true,
          order: route.meta?.menuGroupOrder ?? route.meta?.order ?? 0,
        },
        children: [],
      } as RouteRecordRaw);

    groupRoute.children?.push(route);
    groupMap.set(menuGroup, groupRoute);

    if (!groupedRoutes.includes(groupRoute)) {
      groupedRoutes.push(groupRoute);
    }
  });

  return groupedRoutes;
};

export default function useMenuTree() {
  const permission = usePermission();
  const appStore = useAppStore();
  const appRoute = computed(() => {
    if (appStore.menuFromServer) {
      return appStore.appAsyncMenus;
    }
    return appClientMenus;
  });
  const menuTree = computed(() => {
    const copyRouter = cloneDeep(appRoute.value) as RouteRecordNormalized[];
    copyRouter.sort((a: RouteRecordNormalized, b: RouteRecordNormalized) => {
      return (a.meta.order || 0) - (b.meta.order || 0);
    });
    function travel(_routes: RouteRecordRaw[], layer: number) {
      if (!_routes) return [];

      const collector: any = _routes.map((element) => {
        // no access
        if (!permission.accessRouter(element)) {
          return null;
        }

        // route filter hideInMenu true
        if (element.meta?.hideInMenu === true) {
          return null;
        }

        // leaf node
        if (element.meta?.hideChildrenInMenu || !element.children) {
          element.children = [];
          return element;
        }

        // filter hidden child routes before grouping or flattening
        element.children = element.children.filter((x) => x.meta?.hideInMenu !== true);

        // Associated child node
        const subItem = travel(element.children, layer + 1);

        if (subItem.length) {
          if (subItem.length === 1 && shouldFlattenSingleChild(element, layer)) {
            return inheritSingleChildMeta(element, subItem[0]);
          }

          element.children = subItem;
          return element;
        }
        // the else logic
        if (layer > 1) {
          element.children = subItem;
          return element;
        }

        if (element.meta?.hideInMenu === false) {
          return element;
        }

        return null;
      });
      return collector.filter(Boolean);
    }
    return groupTopLevelMenus(travel(copyRouter, 0));
  });

  return {
    menuTree,
  };
}
