// The tree-shaken ECharts build (spec 11.1): only what the charts use. Loaded lazily by
// useChart so the initial bundle stays inside its budget (spec 14).
import { BarChart, LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { connect, init, use } from 'echarts/core'
import { SVGRenderer } from 'echarts/renderers'

use([BarChart, LineChart, GridComponent, TooltipComponent, SVGRenderer])

export { connect, init }
