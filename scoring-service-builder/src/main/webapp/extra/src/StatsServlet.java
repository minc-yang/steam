import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.reflect.TypeToken;

import java.io.*;
import java.lang.reflect.Type;
import java.util.Map;
import java.util.HashMap;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.TimeZone;
import javax.servlet.http.*;
import javax.servlet.*;

public class StatsServlet extends HttpServlet {

  public static final Gson gson = new GsonBuilder().serializeSpecialFloatingPointValues().create();

  public void doGet(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
    try {
      if (ServletUtil.model == null)
        throw new Exception("No predictor model");

      final long now = System.currentTimeMillis();
      final long upTimeMs = now - ServletUtil.startTime;
      final long lastTimeAgoMs = now - ServletUtil.lastTime;
      SimpleDateFormat sdf = new SimpleDateFormat("yyyy-MM-dd HH:mm:ss z");
      sdf.setTimeZone(TimeZone.getTimeZone("UTC"));
      final String startUTC = sdf.format(new Date(ServletUtil.startTime));
      final String lastPredictionUTC = ServletUtil.lastTime > 0 ? sdf.format(new Date(ServletUtil.lastTime)) : "";
      final long warmUpCount = ServletUtil.warmUpCount;

      Map<String, Object> js = new HashMap<String, Object>() {
        {
          put("startTime", ServletUtil.startTime);
          put("lastTime", ServletUtil.lastTime);
          put("lastTimeUTC", lastPredictionUTC);
          put("startTimeUTC", startUTC);
          put("upTimeMs", upTimeMs);
          put("lastTimeAgoMs", lastTimeAgoMs);
          put("lastTimeAgoMs", lastTimeAgoMs);
          put("warmUpCount", warmUpCount);

          put("prediction", ServletUtil.predictionTimes.toMap());
          put("get", ServletUtil.getTimes.toMap());
          put("post", ServletUtil.postTimes.toMap());
          put("pythonget", ServletUtil.getPythonTimes.toMap());
          put("pythonpost", ServletUtil.postPythonTimes.toMap());
        }
      };
      String json = gson.toJson(js, ServletUtil.mapType);

      response.getWriter().write(json);
      response.setStatus(HttpServletResponse.SC_OK);
    }
    catch (Exception e) {
      // Prediction failed.
      System.out.println(e.getMessage());
      response.sendError(HttpServletResponse.SC_NOT_ACCEPTABLE, e.getMessage());
    }
  }

}
